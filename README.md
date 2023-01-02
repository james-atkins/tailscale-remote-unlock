# tailscale-remote-unlock - Unlock encrypted volumes at boot over Tailscale

When I encrypt the root partition on a Linux computer, I need to supply the passphrase at boot to unlock the sytem for startup to continue.
This all well and good if I'm sitting in front of the machine, but what if I'm at the beach on the other side of the world?

`tailscale-remote-unlock` lets you unlock encrypted volumes over Tailscale.
It aims to be more robust and user friendly than the usual collection of shell scripts that are usually used for this purpose.

**Disclaimer**: I am not associated with Tailscale and this project is not officially endorsed by them.

## Features

`tailscale-remote-unlock` uses Tailscale's `tsnet` library to create a [virtual private service](https://tailscale.com/blog/tsnet-virtual-private-services/) solely for the purpose of remotely unlocking the machine at boot.
It is a single binary that connects to Tailscale, waits for the password(s) to be entered via SSH or the web server, and then unlocks the encrypted volumes and continues the boot process.

- [x] One password unlocks multiple volumnes
- [x] SSH interface
- [x] ZFS encrypted datasets
- [ ] Web interface
- [ ] dm-crypt encryption
- [ ] DHCP client and network management

