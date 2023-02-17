ptt-fix
=======

This program provides a simple workaround to the problem of push-to-talk in Wayland. It is based on a [C++ implementation by Rush][rush], but adds features such as the ability to listen to multiple devices simultaneously.

Installation
------------

To install, simply run

```bash
$ go install deedles.dev/ptt-fix@latest
```

Usage
-----

First, determine the keysym that represents the key that you'd like to use. This can be found in `/usr/include/xkbcommon/xdkbcommon-keysyms.h` by finding the macro name for a given key and removing `XKB_KEY_` from the front of it. For example, the letter A is just `a`, while a comma is `comma` and left alt is `alt_l`. This is not case sensitive. The example below assumes that you chose the `+` key.

Next, determine the path to the input devices you'd like to use. The below example assumes that your keyboard is at `/dev/input/by-id/fake-kbd`, but it is quite probably different on your system. It is easiest to look in the `/dev/input/by-id` directory for something that looks likely to be your keyboard. It will probably end with `-kbd`.

```bash
ptt-fix -key plus /dev/input/event7
```

And that's it. As long as the program is running, the plus key being pressed on your keyboard will be forwarded to X programs that are listening for it, such as Discord. If you'd like to listen to multiple devices, for example because you have a keyboard button mapped to your mouse and you want both devices to work, just list all of them when running the program.

[rush]: https://github.com/Rush/wayland-push-to-talk-fix
