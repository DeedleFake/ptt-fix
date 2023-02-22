ptt-fix
=======

This program provides a simple workaround to the problem of push-to-talk in Wayland. It is based on a [C++ implementation by Rush][rush], but adds features such as the ability to listen to multiple devices simultaneously and the ability to choose the keys involved at runtime.

Installation
------------

If you are on Arch Linux, ptt-fix is available from the AUR as [`ptt-fix`][aur].

To install manually, you will need `libxdo` installed. Then, simply run

```bash
$ go install deedles.dev/ptt-fix@latest
```

A manual installation of this kind will need to be run as root as regular users don't normally, and shouldn't, have read access to the devices in `/dev/input`.

Usage
-----

The default config uses left alt for push-to-talk, waits 10 seconds before retrying a device that wasn't working, and uses all devices that it finds in `/dev/input/by-id/`. If you would like to modify these settings, first run `ptt-fix -createconfig`. This will write the default config to a file, probably `$HOME/.config/ptt-fix/config` and print the path to that file. The file has lots of comments, so simply open it in the text editor of your choice and modify it however you would like.

Donate
------

<a href="https://www.buymeacoffee.com/DeedleFake" target="_blank"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-green.png" alt="Buy Me A Coffee" style="height: 60px !important;width: 217px !important;" ></a>

[rush]: https://github.com/Rush/wayland-push-to-talk-fix
[aur]: https://aur.archlinux.org/packages/ptt-fix
