package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestZFS(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Fatal("ZFS tests must run as root")
	}

	stderr := &strings.Builder{}
	cmd := exec.Command("/usr/bin/env", "bash", "tests/zfs_volumes.sh", "setup")
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		t.Logf("failed to run script: %s", err)
		t.Logf("stderr:\n%s", stderr.String())
		t.FailNow()
	}

	defer func() {
		stderr := &strings.Builder{}
		cmd := exec.Command("/usr/bin/env", "bash", "tests/zfs_volumes.sh", "cleanup")
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			t.Logf("cleanup failed: %s", err)
			t.Logf("stderr:\n%s", stderr.String())
			t.Fail()
		}
	}()

	zfs, err := NewZFS()
	if err != nil {
		t.Fatal(err)
	}

	volumes, err := zfs.Volumes()
	if err != nil {
		t.Fatal(err)
	}

	assert.Contains(t, volumes, "tsru-test-dozer")
	assert.Contains(t, volumes, "tsru-test-tank/enc")
	assert.Contains(t, volumes, "tsru-test-tank/moreenc")
	assert.Contains(t, volumes, "tsru-test-dozer/subenc")

	assert.NotContains(t, volumes, "tsru-test-tank/noenc")
	assert.NotContains(t, volumes, "tsru-test-tank/enc/inherited")

	for _, unlocked := range volumes {
		assert.False(t, unlocked)
	}

	// Now, enter password for just one volume
	valid, err := zfs.EnterPassword("tsru-test-dozer", "wrong password!!!")
	if err != nil {
		t.Fatal(err)
	}
	assert.False(t, valid)

	valid, err = zfs.EnterPassword("tsru-test-dozer", "password")
	if err != nil {
		t.Fatal(err)
	}
	assert.True(t, valid)

	volumes, err = zfs.Volumes()
	if err != nil {
		t.Fatal(err)
	}
	assert.True(t, volumes["tsru-test-dozer"])
	assert.False(t, volumes["tsru-test-tank/enc"])
	assert.False(t, volumes["tsru-test-tank/moreenc"])
	assert.False(t, volumes["tsru-test-dozer/subenc"])

	// Now enter password for multiple volumes
	numUnlocked, err := EnterPasswordAllVolumes(zfs, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, numUnlocked, 2)

	volumes, err = zfs.Volumes()
	if err != nil {
		t.Fatal(err)
	}
	assert.True(t, volumes["tsru-test-dozer"])
	assert.False(t, volumes["tsru-test-tank/enc"])
	assert.True(t, volumes["tsru-test-tank/moreenc"])
	assert.True(t, volumes["tsru-test-dozer/subenc"])

	numUnlocked, err = EnterPasswordAllVolumes(zfs, "password")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, numUnlocked, 1)

	volumes, err = zfs.Volumes()
	if err != nil {
		t.Fatal(err)
	}
	assert.True(t, volumes["tsru-test-dozer"])
	assert.True(t, volumes["tsru-test-tank/enc"])
	assert.True(t, volumes["tsru-test-tank/moreenc"])
	assert.True(t, volumes["tsru-test-dozer/subenc"])
}