# auth-notify

A small ssh and sudo notification daemon. It parses /var/log/auth.log, searches for successful ssh authentications
and all sudo authentications, and notifies about them both in stdout, and optionally in Telegram.

Currently only tested with OpenSSH and systemd on Debian.

## Installation

Download binary from the release, copy it to `/usr/local/bin/auth-notify`, then copy `auth-notify.conf` into
`/etc/`, and `auth-notify.service` into `/etc/systemd/system/`.

You can start the service by running `systemctl start auth-notify`, and enable automatic start on boot with
`systemctl enable auth-notify`.