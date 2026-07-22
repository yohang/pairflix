package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/yohang/pairflix/internal/engine"
)

// errNoSelection is returned when input ends before a valid choice is made.
var errNoSelection = errors.New("no file selected")

// pickFile returns the torrent file index chosen among candidates. With a
// single candidate it is returned immediately; otherwise the user picks from
// a numbered list read from in.
func pickFile(in io.Reader, out io.Writer, candidates []engine.FileInfo) (int, error) {
	if len(candidates) == 0 {
		return 0, errors.New("no candidates to pick from")
	}

	if len(candidates) == 1 {
		fmt.Fprintf(out, "Streaming: %s (%s)\n", candidates[0].Path, humanBytes(candidates[0].Length))

		return candidates[0].Index, nil
	}

	fmt.Fprintln(out, "Multiple video files found:")

	for i, c := range candidates {
		fmt.Fprintf(out, "  [%d] %s (%s)\n", i+1, c.Path, humanBytes(c.Length))
	}

	scanner := bufio.NewScanner(in)

	for {
		fmt.Fprintf(out, "Select file [1-%d]: ", len(candidates))

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return 0, fmt.Errorf("reading selection: %w", err)
			}

			return 0, errNoSelection
		}

		choice, err := strconv.Atoi(scanner.Text())
		if err != nil || choice < 1 || choice > len(candidates) {
			fmt.Fprintf(out, "Invalid choice %q.\n", scanner.Text())

			continue
		}

		return candidates[choice-1].Index, nil
	}
}
