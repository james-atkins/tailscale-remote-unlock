package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type Unlocker interface {
	// Try and unlock the volume with the given password.
	EnterPassword(volume string, password string) (bool, error)

	// Returns a map of encrypted volumes, and whether they are unlocked or not
	Volumes() (map[string]bool, error)

	ContinueBoot() error
}

var UnlockedAllVolumes = errors.New("unlocked all volumes")

func EnterPasswordAllVolumes(unlocker Unlocker, password string) (int, error) {
	numUnlocked := 0

	volumes, err := unlocker.Volumes()
	if err != nil {
		return numUnlocked, err
	}

	for volume, unlocked := range volumes {
		if unlocked {
			continue
		}

		v, err := unlocker.EnterPassword(volume, password)
		if err != nil {
			return numUnlocked, err
		}
		if v {
			numUnlocked++
		}
	}

	return numUnlocked, nil
}

type ZFS struct {
	zfsPath     string
	zpoolPath   string
	killallPath string
}

func NewZFS() (*ZFS, error) {
	zfsPath, err := exec.LookPath("zfs")
	if err != nil {
		return nil, err
	}

	zpoolPath, err := exec.LookPath("zpool")
	if err != nil {
		return nil, err
	}

	killallPath, err := exec.LookPath("killall")
	if err != nil {
		return nil, err
	}

	// Test that zfs and zpool commands work, i.e. that the kernel modules are loaded
	if err := exec.Command(zfsPath, "version").Run(); err != nil {
		return nil, err
	}

	if err := exec.Command(zpoolPath, "version").Run(); err != nil {
		return nil, err
	}

	return &ZFS{zfsPath: zfsPath, zpoolPath: zpoolPath, killallPath: killallPath}, nil
}

func (zfs *ZFS) EnterPassword(volume string, password string) (bool, error) {
	valid, err := zfs.tryUnlock(volume, password)
	if err != nil {
		return false, err
	}

	return valid, nil
}

func (zfs *ZFS) Volumes() (map[string]bool, error) {
	encRoots, err := zfs.getEncryptionRoots()
	if err != nil {
		return nil, err
	}

	keyStatuses, err := zfs.getKeyStatuses(encRoots)
	if err != nil {
		return nil, err
	}

	return keyStatuses, nil
}

func (zfs *ZFS) getEncryptionRoots() ([]string, error) {
	cmd := exec.Command(zfs.zfsPath, "get", "encryptionroot", "-H", "-o", "value")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	encRootsMap := make(map[string]struct{})

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		text := scanner.Text()
		if text != "-" {
			encRootsMap[text] = struct{}{}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	encRoots := make([]string, 0, len(encRootsMap))
	for encRoot := range encRootsMap {
		encRoots = append(encRoots, encRoot)
	}

	return encRoots, nil
}

func (zfs *ZFS) getKeyStatuses(encRoots []string) (map[string]bool, error) {
	// Short circuit if there are no encrypted volumes
	if len(encRoots) == 0 {
		return make(map[string]bool), nil
	}

	cmd := exec.Command(zfs.zfsPath, "get", "keystatus", "-H", "-o", "name,value")
	cmd.Args = append(cmd.Args, encRoots...)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	r := csv.NewReader(bytes.NewReader(output))
	r.Comma = '\t'
	r.FieldsPerRecord = 2

	keyStatuses := make(map[string]bool)

	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		encRoot, value := record[0], record[1]

		if value == "available" {
			keyStatuses[encRoot] = true
		} else if value == "unavailable" {
			keyStatuses[encRoot] = false
		} else {
			// none is not a valid option for encryption roots
			return nil, fmt.Errorf("zfs get keystatus returned unexpected value: %s = %s", encRoot, value)
		}
	}

	return keyStatuses, nil
}

func (zfs *ZFS) tryUnlock(encRoot string, password string) (bool, error) {
	stderr := &strings.Builder{}
	cmd := exec.Command(zfs.zfsPath, "load-key", encRoot)
	cmd.Stdin = strings.NewReader(password)
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if exitError.ExitCode() > 0 {
				// We need to check the actual error messages :(
				// https://github.com/openzfs/zfs/blob/zfs-2.1.7/lib/libzfs/libzfs_crypto.c#L1395
				// https://github.com/openzfs/zfs/blob/zfs-2.1.7/lib/libzfs/libzfs_crypto.c#L256
				msg := strings.TrimSpace(stderr.String())
				if strings.HasPrefix(msg, "Key load error: Passphrase too ") || strings.HasPrefix(msg, "Key load error: Incorrect key provided for ") {
					return false, nil
				}

				return false, fmt.Errorf(msg)
			}
		}
	}

	return true, nil
}

func (zfs *ZFS) ContinueBoot() error {
	return exec.Command(zfs.killallPath, "systemd-ask-password", "zfs").Run()
}
