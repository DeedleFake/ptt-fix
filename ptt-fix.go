package main

/*
#cgo pkg-config: libxdo

#include <malloc.h>
#include <errno.h>
#include <linux/input.h>
#include <xdo.h>
*/
import "C"
import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sync/errgroup"
)

const (
	eventInvalid = iota
	eventUp
	eventDown
)

func listen(ctx context.Context, device string, keycode uint16, out chan<- int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	d, err := OpenDevice(device)
	if err != nil {
		panic(err)
	}
	defer d.Close()

	go func() {
		<-ctx.Done()
		d.Close()
	}()

	log.Printf(
		"initialized input device %q, name: %q, bus: %x, vendor: %x, product: %x",
		device,
		d.Name,
		d.BusType,
		d.Vendor,
		d.Product,
	)

	if !d.HasEventCode(C.EV_KEY, keycode) {
		log.Printf("device %q (%v) is not capable of sending requested key code, ignoring", device, d.Name)
		return nil
	}

	for {
		ev, err := d.NextEvent()
		if err != nil {
			if ctx.Err() != nil {
				return err
			}

			log.Printf("read event from %q: %v", d.Name, err)
			continue
		}

		if !ev.Is(C.EV_KEY, keycode) {
			continue
		}

		switch ev.Value {
		case 2:
		case 1:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case out <- eventDown:
			}
		default:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case out <- eventUp:
			}
		}
	}

	return nil
}

func handle(ctx context.Context, xdo *C.struct_xdo, key *C.char, ev <-chan int) error {
	defer C.xdo_free(xdo)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev := <-ev:
			switch ev {
			case eventUp:
				C.xdo_send_keysequence_window_up(xdo, C.CURRENTWINDOW, key, 0)
				log.Printf("deactivated")
			case eventDown:
				C.xdo_send_keysequence_window_down(xdo, C.CURRENTWINDOW, key, 0)
				log.Println("activated")
			default:
				return fmt.Errorf("invalid event: %v", ev)
			}
		}
	}
}

func findDevices() ([]string, error) {
	files, err := os.ReadDir("/dev/input")
	if err != nil {
		return nil, err
	}

	devices := make([]string, 0, len(files))
	for _, f := range files {
		if f.IsDir() || !strings.HasPrefix(f.Name(), "event") {
			continue
		}

		devices = append(devices, filepath.Join("/dev/input", f.Name()))
	}

	return devices, nil
}

func run(ctx context.Context) error {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [/dev/input/by-id/<device>...]\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
	}
	key := flag.Uint("key", 56, "keycode to watch for")
	sym := flag.String("sym", "Alt_L", "key symbol to send to X")
	flag.Parse()

	devices := flag.Args()
	if len(devices) == 0 {
		d, err := findDevices()
		if err != nil {
			return fmt.Errorf("find devices: %w", err)
		}
		if len(d) == 0 {
			return errors.New("no devices found")
		}

		devices = d
	}

	xdo := C.xdo_new(nil)
	if xdo == nil {
		return errors.New("initialize xdo")
	}

	xdokey := C.CString(*sym)
	defer C.free(unsafe.Pointer(xdokey))

	eg, ctx := errgroup.WithContext(ctx)

	ev := make(chan int)
	for _, dev := range devices {
		dev := dev
		eg.Go(func() error { return listen(ctx, dev, uint16(*key), ev) })
	}
	eg.Go(func() error { return handle(ctx, xdo, xdokey, ev) })

	err := eg.Wait()
	if (err != nil) && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}

func main() {
	if addr := os.Getenv("PPROF_ADDR"); addr != "" {
		go func() { log.Fatalln(http.ListenAndServe(addr, nil)) }()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := run(ctx)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
}
