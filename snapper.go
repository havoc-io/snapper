package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	// rsyncCommand is the command to use to invoke rsync.
	rsyncCommand = "rsync"

	// rsyncArchiveFlags are the behavioral flags to pass to rsync for archiving
	// behavior. The "-a" flag is the standard archiving configuration (see
	// "man rsync" for more details), the "-P" flag displays, and the "-h" flag
	// shows numbers in human-readable format.
	rsyncArchiveFlags = "-aPh"

	// rsyncDisableSpecials is the flag to disable copying special files (e.g.
	// sockets and FIFOs).
	rsyncDisableSpecials = "--no-specials"

	// rsyncDisableDevices is the flag to disable copying device files.
	rsyncDisableDevices = "--no-devices"

	// rsyncBaseFlagFormat is a format string for the flag to use to tell rsync
	// to use a path as a base for snapshots.
	rsyncBaseFlagFormat = "--link-dest=%s"

	// rsyncExcludeFlagFormat is a format string for the flag to use to tell
	// rsync to exclude a path.
	rsyncExcludeFlagFormat = "--exclude=%s"

	// snapshotPermissions are the permissions to use for the snapshots root and
	// individual snapshot roots.
	snapshotPermissions = 0700

	// latestSnapshotLinkName is the name of the symlink to the latest snapshot
	// in the snapshots directory.
	latestSnapshotLinkName = "Latest"
)

var usage = `usage: snapper [-h|--help] [-exclude=<excluded-path>] <root> <snapshots>`

type excludes []string

func (e *excludes) String() string {
	return "excluded paths"
}

func (e *excludes) Set(value string) error {
	*e = append(*e, value)
	return nil
}

func main() {
	// Parse command line arguments.
	var excludes excludes
	flags := flag.NewFlagSet("snapper", flag.ContinueOnError)
	flags.Usage = func() {}
	flags.SetOutput(ioutil.Discard)
	flags.Var(&excludes, "exclude", "adds a path (relative to root) to be exclude")
	if err := flags.Parse(os.Args[1:]); err == flag.ErrHelp {
		fmt.Println(usage)
		os.Exit(0)
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}
	arguments := flags.Args()
	if len(arguments) != 2 {
		fmt.Fprintln(os.Stderr, "error: invalid number of positional arguments")
		os.Exit(1)
	}
	root := arguments[0]
	if root == "" {
		fmt.Fprintln(os.Stderr, "error: empty root path")
		os.Exit(1)
	}
	snapshotsDirectory := arguments[1]
	if snapshotsDirectory == "" {
		fmt.Fprintln(os.Stderr, "error: empty snapshots directory path")
		os.Exit(1)
	}

	// Ensure that the snapshots root exists.
	if err := os.MkdirAll(snapshotsDirectory, snapshotPermissions); err != nil {
		fmt.Fprintln(os.Stderr, "error: unable to create snapshots directory:", err)
		os.Exit(1)
	}

	// Create base rsync arguments.
	rsyncArguments := []string{rsyncArchiveFlags, rsyncDisableSpecials, rsyncDisableDevices}

	// Check if there's already an existing backup. If so, tell rsync to use it
	// as a hardlink base.
	lastestSnapshotLink := filepath.Join(snapshotsDirectory, latestSnapshotLinkName)
	if stat, err := os.Lstat(lastestSnapshotLink); err == nil {
		if stat.Mode()&os.ModeSymlink == 0 {
			fmt.Fprintln(os.Stderr, "error: latest backup link path exists but is not a symlink")
			os.Exit(1)
		}
		// TODO: Should we add an os.Readlink call in here to ensure the symlink
		// is sane? Or will rsync do that?
		rsyncArguments = append(rsyncArguments, fmt.Sprintf(rsyncBaseFlagFormat, lastestSnapshotLink))
	} else if !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "error: unable to inspect latest backup link path:", err)
		os.Exit(1)
	}

	// Add excluded paths.
	for _, p := range excludes {
		rsyncArguments = append(rsyncArguments, fmt.Sprintf(rsyncExcludeFlagFormat, p))
	}

	// Add the root path, but ensure that it has a trailing slash, because we
	// want its contents to go directly into the snapshot root. rsync is
	// sensitive to whether or not the source ends with a trailing slash. It
	// doesn't care whether or not the destination has a trailing slash:
	//	http://defindit.com/readme_files/rsync_backup.html
	if root[len(root)-1] != '/' {
		root += "/"
	}
	rsyncArguments = append(rsyncArguments, root)

	// Compute the date, convert it to a UTC ISO-8601 timestamp (Go uses this
	// weird WYSIWYG timestamp formatting string), and use that as the snapshot
	// name. Attempt to create the directory, aborting if that's not possible.
	// If all succeeds, then add the destination argument.
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	snapshot := filepath.Join(snapshotsDirectory, timestamp)
	if err := os.Mkdir(snapshot, snapshotPermissions); err != nil {
		fmt.Fprintln(os.Stderr, "error: unable to create snapshot root:", err)
		os.Exit(1)
	}
	rsyncArguments = append(rsyncArguments, snapshot)

	// Run rsync.
	rsync := exec.Command(rsyncCommand, rsyncArguments...)
	rsync.Stdin = os.Stdin
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error: rsync execution error:", err)
		os.Exit(1)
	}

	// Update the last backup link.
	if err := os.Remove(lastestSnapshotLink); err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "error: unable to remove latest backup link:", err)
		os.Exit(1)
	} else if err = os.Symlink(timestamp, lastestSnapshotLink); err != nil {
		fmt.Fprintln(os.Stderr, "error: unable to update latest backup link:", err)
		os.Exit(1)
	}
}
