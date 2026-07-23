package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/yohang/pairflix/internal/cast"
	"github.com/yohang/pairflix/internal/engine"
	"github.com/yohang/pairflix/internal/units"
)

// errNoSelection is returned when input ends before a valid choice is made.
var errNoSelection = errors.New("no selection made")

// pick returns the index chosen among labels via a numbered prompt on in.
// A single label is chosen immediately without prompting.
func pick(in io.Reader, out io.Writer, header string, labels []string) (int, error) {
	if len(labels) == 0 {
		return 0, errors.New("nothing to pick from")
	}

	if len(labels) == 1 {
		return 0, nil
	}

	fmt.Fprintln(out, header)

	for i, label := range labels {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, label)
	}

	scanner := bufio.NewScanner(in)

	for {
		fmt.Fprintf(out, "Select [1-%d]: ", len(labels))

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return 0, fmt.Errorf("reading selection: %w", err)
			}

			return 0, errNoSelection
		}

		choice, err := strconv.Atoi(scanner.Text())
		if err != nil || choice < 1 || choice > len(labels) {
			fmt.Fprintf(out, "Invalid choice %q.\n", scanner.Text())

			continue
		}

		return choice - 1, nil
	}
}

// pickFile returns the torrent file index chosen among candidates.
func pickFile(in io.Reader, out io.Writer, candidates []engine.FileInfo) (int, error) {
	labels := make([]string, len(candidates))
	for i, c := range candidates {
		labels[i] = fmt.Sprintf("%s (%s)", c.Path, units.HumanBytes(c.Length))
	}

	i, err := pick(in, out, "Multiple video files found:", labels)
	if err != nil {
		return 0, err
	}

	fmt.Fprintf(out, "Streaming: %s (%s)\n", candidates[i].Path, units.HumanBytes(candidates[i].Length))

	return candidates[i].Index, nil
}

// pickDevice returns the Chromecast device chosen among devices.
func pickDevice(in io.Reader, out io.Writer, devices []cast.Device) (cast.Device, error) {
	labels := make([]string, len(devices))
	for i, d := range devices {
		labels[i] = fmt.Sprintf("%s (%s, %s)", d.Name, d.Model, d.Addr)
	}

	i, err := pick(in, out, "Multiple Chromecast devices found:", labels)
	if err != nil {
		return cast.Device{}, err
	}

	fmt.Fprintf(out, "Casting to: %s (%s)\n", devices[i].Name, devices[i].Model)

	return devices[i], nil
}
