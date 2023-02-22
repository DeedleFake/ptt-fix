package config

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed default
var defaultFile string

type Config struct {
	Key     uint
	Sym     Sym
	Retry   time.Duration
	Devices []string
}

func DefaultFile() string {
	return defaultFile
}

func DefaultPath() (string, error) {
	c, err := os.UserConfigDir()
	return filepath.Join(c, "ptt-fix", "config"), err
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	return Parse(file)
}

func Parse(r io.Reader) (c Config, err error) {
	var num int
	s := bufio.NewScanner(r)
	for s.Scan() {
		num++

		line := strings.TrimSpace(s.Text())
		if (len(line) == 0) || (line[0] == '#') {
			continue
		}

		directive, rem, _ := strings.Cut(line, " ")
		switch directive {
		case "key":
			err = c.key(rem)
		case "sym":
			err = c.sym(rem)
		case "retry":
			err = c.retry(rem)
		case "device":
			err = c.device(rem)
		default:
			return c, fmt.Errorf("unknown directive %q on line %v", directive, line)
		}
		if err != nil {
			return c, fmt.Errorf("line %v: %w", num, err)
		}
	}
	if err := s.Err(); err != nil {
		return c, fmt.Errorf("scan: %w", err)
	}

	return c, nil
}

func (c *Config) key(str string) error {
	if c.Key != 0 {
		return errors.New("attempted to set key twice")
	}

	v, err := strconv.ParseUint(str, 0, 0)
	if err != nil {
		return fmt.Errorf("parse key: %w", err)
	}
	c.Key = uint(v)
	return nil
}

func (c *Config) sym(str string) error {
	if c.Sym != (Sym{}) {
		return errors.New("attempted to set sym twice")
	}

	t, v, ok := strings.Cut(str, " ")
	if !ok {
		v = t
		t = "key"
	}
	c.Sym = Sym{Type: t, Val: v}
	return nil
}

func (c *Config) retry(str string) error {
	if c.Retry != 0 {
		return errors.New("attempted to set retry twice")
	}

	r, err := time.ParseDuration(str)
	if err != nil {
		return fmt.Errorf("parse retry: %w", err)
	}
	c.Retry = r
	return nil
}

func (c *Config) device(str string) error {
	m, err := filepath.Glob(str)
	if err != nil {
		return fmt.Errorf("find devices: %w", err)
	}
	c.Devices = append(c.Devices, m...)
	return nil
}

type Sym struct {
	Type string
	Val  string
}
