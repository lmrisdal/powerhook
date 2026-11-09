//go:build !windows

package service

import "os"

func openTerminal() (in, out *os.File, err error) {
	in, err = os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	out, err = os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		in.Close()
		return nil, nil, err
	}
	return in, out, nil
}
