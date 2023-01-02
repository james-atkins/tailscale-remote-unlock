package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"sort"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/sync/errgroup"
	"tailscale.com/client/tailscale"
)

func runSSH(ctx context.Context, ln net.Listener, unlocker Unlocker, lc *tailscale.LocalClient, hostKeys []gossh.Signer) error {
	// This channel is closed if and when all the volumes have been unlocked
	successC := make(chan struct{})

	handler := func(sess ssh.Session) {
		ptyReq, winCh, isPty := sess.Pty()
		if isPty {
			term := terminal.NewTerminal(sess, "")
			term.SetSize(ptyReq.Window.Width, ptyReq.Window.Height)

			go func() {
				// winCh is automatically closed when the handler function returns, so this goroutine terminates
				for win := range winCh {
					term.SetSize(win.Width, win.Height)
				}
			}()

			fmt.Fprintf(term, "🔑 Welcome to tailscale-remote-unlock!\n")

			for {
				volumes, err := unlocker.Volumes()
				if err != nil {
					fmt.Fprintf(term, "Error getting encrypted volumes: %+v\n", err)
					return
				}

				// Pretty print which volumes are locked and unlocked
				{
					numLocked := 0
					vs := make([]string, 0, len(volumes))
					for v, unlocked := range volumes {
						vs = append(vs, v)
						if !unlocked {
							numLocked++
						}
					}

					if numLocked == 0 {
						// We are done!
						fmt.Fprintf(term, "%s\n✓ All volumes are unlocked.%s\n", term.Escape.Green, term.Escape.Reset)
						close(successC)
						return
					}

					sort.Strings(vs)

					fmt.Fprintf(term, "\n")
					for _, v := range vs {
						unlocked := volumes[v]
						if unlocked {
							fmt.Fprintf(term, "%s⬤ %s %s\n", term.Escape.Green, term.Escape.Reset, v)
						} else {
							fmt.Fprintf(term, "%s⬤ %s %s\n", term.Escape.Red, term.Escape.Reset, v)
						}
					}
					fmt.Fprintf(term, "\n")
				}

			PasswordLoop:
				for {
					password, err := term.ReadPassword("Password: ")
					if err == io.EOF {
						// Ctrl-C
						fmt.Fprintf(term, "\n")
						return
					}
					if password == "" {
						fmt.Fprintf(term, "%sInvalid password. Please try again.%s\n", term.Escape.Red, term.Escape.Reset)
						continue PasswordLoop
					}

					numUnlocked, err := EnterPasswordAllVolumes(unlocker, password)
					if err != nil {
						fmt.Fprintf(term, "%sError: %+v%s\n", term.Escape.Red, err, term.Escape.Reset)
						continue
					}

					if numUnlocked > 0 {
						fmt.Fprintf(term, "%sUnlocked %d volumes.%s\n", term.Escape.Green, numUnlocked, term.Escape.Reset)
						break PasswordLoop
					} else {
						fmt.Fprintf(term, "%sInvalid password. Please try again.%s\n", term.Escape.Red, term.Escape.Reset)
					}
				}

			}
		} else {
			// TODO: read input and try to unlock without any terminal fancyness, e.g. if piped password to ssh command
			io.WriteString(sess, "No PTY requested.\n")
			sess.Exit(1)
		}
	}

	// Callback for allowing or denying SSH sessions
	sessionRequestCallback := func(sess ssh.Session, requestType string) bool {
		if requestType == "x11-req" || requestType == "subsystem" {
			return false
		}
		// Check Tailscale identity
		// who, err := lc.WhoIs(sess.Context(), sess.RemoteAddr().String())
		// if err != nil {
		// 	http.Error(w, err.Error(), 500)
		// 	return
		// }

		return true
	}

	errgrp, ctx := errgroup.WithContext(ctx)

	srv := &ssh.Server{
		Handler:                handler,
		SessionRequestCallback: sessionRequestCallback,
	}

	for _, hostKey := range hostKeys {
		srv.AddHostKey(hostKey)
	}

	// Goroutine to run the server
	errgrp.Go(func() error {
		if err := srv.Serve(ln); err != ssh.ErrServerClosed {
			return err
		}
		return nil
	})

	// Goroutine to notify when all volumes have been successfully unlocked
	errgrp.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, more := <-successC:
			if !more {
				return UnlockedAllVolumes
			}
			return nil
		}
	})

	// Goroutine to shutdown the server when the context is done
	errgrp.Go(func() error {
		<-ctx.Done()
		return srv.Close()
	})

	return errgrp.Wait()
}
