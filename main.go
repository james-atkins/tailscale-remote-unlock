package main

import (
	"bytes"
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
	"tailscale.com/tsnet"
)

type stringFlags struct {
	set    bool
	Values []string
}

func newStringFlags(defaults ...string) stringFlags {
	return stringFlags{
		set:    false,
		Values: defaults,
	}
}

func (sf *stringFlags) String() string {
	return "[" + strings.Join(sf.Values, ", ") + "]"
}

func (sf *stringFlags) Set(value string) error {
	if !sf.set {
		sf.Values = make([]string, 0, 1)
	}
	sf.set = true
	sf.Values = append(sf.Values, value)
	return nil
}

func configFile(fileName string) string {
	return path.Join("/etc/tailscale-remote-unlock", fileName)
}

func main() {
	fs := flag.NewFlagSet("tailscale-remote-unlock", flag.ContinueOnError)

	var (
		hostname        string
		authKeyPath     string
		sshHostKeyPaths stringFlags = newStringFlags(configFile("ssh_host_rsa_key"), configFile("ssh_host_ed25519_key"))
	)

	fs.StringVar(&hostname, "hostname", "", "hostname")
	fs.StringVar(&authKeyPath, "auth-key", configFile("auth_key"), "path to auth key file")
	fs.Var(&sshHostKeyPaths, "ssh-host-key", "path to SSH host key")

	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Print(err)
		return
	}

	if hostname == "" {
		log.Print("hostname must be specified")
		return
	}

	authKey, err := os.ReadFile(authKeyPath)
	if err != nil {
		log.Print(err)
		return
	}
	authKey = bytes.TrimSpace(authKey)

	var hostKeys []gossh.Signer
	for _, sshHostKeyPath := range sshHostKeyPaths.Values {
		content, err := os.ReadFile(sshHostKeyPath)
		if err != nil {
			log.Print(err)
			return
		}

		hostKey, err := gossh.ParsePrivateKey(content)
		if err != nil {
			log.Print(err)
			return
		}

		hostKeys = append(hostKeys, hostKey)
	}

	zfs, err := NewZFS()
	if err != nil {
		log.Print(err)
		return
	}

	config := Config{
		Hostname: hostname,
		Unlocker: zfs,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Exit on signals
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	defer func() {
		signal.Stop(signalChan)
		close(signalChan)
	}()

	go func() {
		// First signal: exit gracefully
		select {
		case <-signalChan:
			log.Print("Exiting...")
			cancel()
		case <-ctx.Done():
			return
		}

		// Second signal: force exit
		_, more := <-signalChan
		if more {
			os.Exit(1)
		}
	}()

	if err := startServer(ctx, config, string(authKey), hostKeys); err != nil {
		log.Print(err)
		return
	}

	return
}

type Config struct {
	Hostname string
	Unlocker Unlocker
}

func startServer(ctx context.Context, config Config, authKey string, hostKeys []gossh.Signer) error {
	ts := &tsnet.Server{
		Hostname:  config.Hostname,
		AuthKey:   authKey,
		Ephemeral: true,
	}

	// Try and connect to the tailnet - this will fail if there is no internet connection
	if err := ts.Start(); err != nil {
		return err
	}
	defer ts.Close()

	client, err := ts.LocalClient()
	if err != nil {
		return err
	}

	// Don't use the ctx context as that might have been cancelled and then we would never be
	// logged out.
	defer client.Logout(context.Background())

	// Now run the SSH service
	errgrp, ctx := errgroup.WithContext(ctx)

	errgrp.Go(func() error {
		ln, err := ts.Listen("tcp", ":22")
		if err != nil {
			return err
		}
		defer ln.Close()

		return runSSH(ctx, ln, config.Unlocker, client, hostKeys)
	})

	err = errgrp.Wait()
	if err != UnlockedAllVolumes {
		// This generally happens if the process has received a SIGINT signal and is shutting down
		return err
	}

	return config.Unlocker.ContinueBoot()
}
