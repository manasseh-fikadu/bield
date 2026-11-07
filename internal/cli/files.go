package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/manasseh-fikadu/bield/internal/api"
)

func fileCommands() []*Command {
	return []*Command{
		{
			Name: "files ls", Args: "<device> [path]", Min: 1, Max: 2, Admin: true, Run: runFilesLs,
			Summary: "list a folder on a device",
			What:    "Bield logs in to the device over SSH as the user an admin set up. Without a path, it's that user's home folder.",
		},
		{Name: "files cat", Args: "<device> <path>", Min: 2, Max: 2, Admin: true, Run: runFilesCat, Summary: "print a file on a device"},
		{
			Name: "files get", Args: "<device> <path> [local]", Min: 2, Max: 3, Admin: true, Run: runFilesGet,
			Summary: "download a file from a device",
			What:    "The file is saved in the current folder under its own name, unless you name where. It won't replace a file that is there, unless you say --overwrite.",
			Flags:   []Flag{{Name: "overwrite", Kind: Bool, Usage: "replace the local file if there is one"}},
		},
		{
			Name: "files put", Args: "<device> <local> [folder]", Min: 2, Max: 3, Admin: true, Run: runFilesPut,
			Summary: "upload a file to a folder on a device",
			What: "Without a folder, it goes to the home folder of the user Bield logs in as. A local file called - is read from standard input, " +
				"and needs --name. A file that is already there is not replaced, unless you say --overwrite or --rename.",
			Flags: []Flag{
				{Name: "name", Arg: "name", Usage: "the name to give the file on the device (default: the local file's name)"},
				{Name: "overwrite", Kind: Bool, Usage: "replace a file with the same name"},
				{Name: "rename", Kind: Bool, Usage: "give the new file a free name, such as a (1).txt"},
			},
		},
		{
			Name: "files write", Args: "<device> <path>", Min: 2, Max: 2, Admin: true, Destructive: true, Run: runFilesWrite,
			Summary: "replace a text file on a device with standard input",
			What:    "For config files and scripts, up to the size the web editor opens. Use files put for anything else.",
			Flags:   []Flag{{Name: "create", Kind: Bool, Usage: "make the file if it isn't there"}},
		},
	}
}

func runFilesLs(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	dir := ""
	if len(c.Args) > 1 {
		dir = c.Args[1]
	}
	l, err := cl.ListFiles(c.Ctx, d.ID, dir)
	if err != nil {
		return err
	}
	slices.SortFunc(l.Entries, func(a, b api.FileEntry) int {
		if (a.Type == api.FileTypeDir) != (b.Type == api.FileTypeDir) {
			if a.Type == api.FileTypeDir {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return c.Print(l, func(v *View) {
		v.Printf("%s\n", v.Paint(bold, d.Name+":"+l.Path))
		t := v.Table("MODE", "OWNER", "GROUP", "SIZE", "MODIFIED", "NAME")
		for _, e := range l.Entries {
			name := e.Name
			switch e.Type {
			case api.FileTypeDir:
				name = v.Paint(bold, name+"/")
			case api.FileTypeSymlink:
				name += " -> " + e.Target
			default:
			}
			t.Row(e.Mode, e.Owner, e.Group, bytesLabel(uint64(max(e.Size, 0))), clock(e.ModifiedAt), name)
		}
		t.Write()
		if l.Space != nil {
			v.Printf("\n%s free of %s\n", bytesLabel(l.Space.Free), bytesLabel(l.Space.Total))
		}
	})
}

func runFilesCat(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	body, err := cl.ReadFile(c.Ctx, d.ID, c.Args[1])
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	_, err = io.Copy(c.Env.Stdout, body)
	return err
}

func runFilesGet(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	dest := path.Base(c.Args[1])
	if len(c.Args) > 2 {
		dest = c.Args[2]
		if fi, err := os.Stat(dest); err == nil && fi.IsDir() {
			dest = filepath.Join(dest, path.Base(c.Args[1]))
		}
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	body, err := cl.ReadFile(c.Ctx, d.ID, c.Args[1])
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if c.Bool("overwrite") {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(dest, flags, 0o644) //nolint:gosec // G302/G304: the person running bield names the file.
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s is already there; say --overwrite to replace it", dest)
		}
		return err
	}
	n, err := io.Copy(f, body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dest)
		return err
	}
	return c.Print(map[string]any{"path": dest, "size": n}, func(v *View) {
		v.Printf("Saved %s (%s)\n", dest, bytesLabel(uint64(max(n, 0))))
	})
}

func runFilesPut(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	local := c.Args[1]
	name := c.Str("name")
	var src io.Reader
	if local == "-" {
		if name == "" {
			return usagef(c.cmd, "a file from standard input needs --name")
		}
		src = c.Env.Stdin
	} else {
		f, err := os.Open(local) //nolint:gosec // G304: the person running bield names the file.
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if fi, err := f.Stat(); err == nil && fi.IsDir() {
			return fmt.Errorf("%s is a folder; files put sends one file", local)
		}
		src = f
		if name == "" {
			name = filepath.Base(local)
		}
	}
	conflict := ""
	switch {
	case c.Bool("overwrite") && c.Bool("rename"):
		return usagef(c.cmd, "--overwrite and --rename can't both be given")
	case c.Bool("overwrite"):
		conflict = "overwrite"
	case c.Bool("rename"):
		conflict = "rename"
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	var dir string
	if len(c.Args) > 2 {
		dir = c.Args[2]
	} else {
		l, err := cl.ListFiles(c.Ctx, d.ID, "")
		if err != nil {
			return err
		}
		dir = l.Home
	}
	up, err := cl.UploadFile(c.Ctx, d.ID, dir, name, conflict, src)
	if err != nil {
		return err
	}
	return c.Print(up, func(v *View) {
		v.Printf("Uploaded %s (%s) to %s:%s\n", up.Name, bytesLabel(uint64(max(up.Size, 0))), d.Name, up.Path)
	})
}

func runFilesWrite(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	// The whole text is read first, so Bield knows its size, and a half-read
	// input doesn't leave half a file.
	data, err := io.ReadAll(io.LimitReader(c.Env.Stdin, 8<<20))
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	e, err := cl.WriteFile(c.Ctx, d.ID, c.Args[1], c.Bool("create"), bytes.NewReader(data))
	if err != nil {
		return err
	}
	return c.Print(e, func(v *View) { v.Printf("Wrote %s (%s) on %s\n", e.Path, bytesLabel(uint64(max(e.Size, 0))), d.Name) })
}
