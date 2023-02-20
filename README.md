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

Usage
-----

First, determine the keycode that represents the key that you'd like to press to activate push-to-talk. This can be found in `/usr/include/linux/input-event-codes.h` as the number that corresponds to the macro named for that key. For example, the letter A is 30 while the comma key is 51. The example below assumes that you chose the minus key.

Once you've done that, find the symbol name that represents the key that Discord is set up to use for push-to-talk. This does not have to be the same key as the one in the first step, but it probably should be for simplicity's sake. The symbol can be found in `/usr/include/xkbcommon/xkbcommon-keysyms.h` as the part of the macro name after `XKB_KEY_`. For example, the minus key is `XKB_KEY_minus`, so the symbol name is `minus`.

Next, you may determine the path to the input devices you'd like to use. This is optional. If you do not specify a device to use, all of the event devices listed in `/dev/input` that support the requested keycode will be used. The below example assumes that your keyboard is at `/dev/input/by-id/fake-kbd`, but it is quite probably different on your system. It is easiest to look in the `/dev/input/by-id` directory for something that looks likely to be your keyboard. It will probably end with `-kbd`.

Finally, actually run the program, passing it the information that you determined in the above steps:

```bash
ptt-fix -key 12 -sym minus /dev/input/event7
```

And that's it. As long as the program is running, the minus key being pressed on your keyboard will be forwarded to X programs that are listening for it, such as Discord. If you'd like to listen to multiple devices, for example because you have a keyboard button mapped to your mouse and you want both devices to work, just list all of them when running the program.

systemd
-------

If you would like to run the app as a service and your Linux installation uses systemd, you can use the following as a starting point for creating your own user service unit by placing it in `$HOME/.config/systemd/user` and editing it as appropriate. The following assumes that ptt-fix was installed via the AUR package which places the binary at `/usr/bin/ptt-fix`. If you installed it via `go install`, it is likely that it is at `$HOME/go/bin/ptt-fix`. Note that the unit file must have an absolute path that does not contain environment variables, so the path would then be something like `/home/<username>/go/bin/ptt-fix`. Also note that the following assumes the use of GNOME. If you use some other compositor, the `WantedBy=` line at the bottom should be changed appropriately.

```systemd
[Unit]
Description=PTT Fix for Wayland

[Service]
ExecStart=/usr/bin/ptt-fix -key 51 -sym comma
Restart=on-failure

[Install]
WantedBy=gnome-session-wayland.target
```

Once the file is in place and edited to your liking, run

```bash
$ systemctl --user enable --now ptt-fix.service
```

to make it automatically start upon logging into a GNOME Wayland session.

Donate
------

<a href="https://www.buymeacoffee.com/DeedleFake" target="_blank"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-green.png" alt="Buy Me A Coffee" style="height: 60px !important;width: 217px !important;" ></a>

[rush]: https://github.com/Rush/wayland-push-to-talk-fix
[aur]: https://aur.archlinux.org/packages/ptt-fix
