package get

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/brimdata/zed/api"
	"github.com/brimdata/zed/api/client"
	"github.com/brimdata/zed/cli/outputflags"
	apicmd "github.com/brimdata/zed/cmd/zed/api"
	"github.com/brimdata/zed/compiler"
	"github.com/brimdata/zed/pkg/charm"
	"github.com/brimdata/zed/pkg/fs"
	"github.com/brimdata/zed/pkg/nano"
	"github.com/brimdata/zed/pkg/storage"
	"github.com/brimdata/zed/zio"
	"github.com/segmentio/ksuid"
)

var Get = &charm.Spec{
	Name:  "get",
	Usage: "get [options] <search>",
	Short: "perform zql searches",
	Long: `
zapi get issues search requests to the zqd search service.

The -from and -to options specify a time range.  If not provided, the entire
pool is searched.

By default, the service streams results in native zng and the zapi client
converts the results to the format specified by -f.

Alternatively, the -e option can specify a different encoding to be used by
the server, in which case the output is not converted by -f.  These two options
cannot be used together.

Statistics and warnings can be displayed on stderr, which can be useful
for long running jobs.

Everything is flow-controlled end to end, so if the client host, network,
or disk becomes a bottleneck, the service will naturally clock itself
at the rate determined by the client.

The -debug option can be useful for debugging.  In this case, the server
response is written unmodified in its entirety to the output.
`,
	New: New,
}

func init() {
	apicmd.Cmd.Add(Get)
	apicmd.Get = Get
}

type Command struct {
	*apicmd.Command
	outputFlags outputflags.Flags
	encoding    string
	from        tsflag
	to          tsflag
	dir         string
	stats       bool
	warnings    bool
	debug       bool
	final       *api.SearchStats
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{
		Command: parent.(*apicmd.Command),
		to:      tsflag(nano.MaxTs),
	}
	f.StringVar(&c.encoding, "e", "zng", "server encoding to use for search results [csv,json,ndjson,zjson,zng]")
	f.BoolVar(&c.stats, "S", false, "display search stats on stderr")
	f.BoolVar(&c.warnings, "W", true, "display warnings on stderr")
	f.BoolVar(&c.debug, "debug", false, "dump raw HTTP response straight to output")
	f.Var(&c.from, "from", "search from timestamp in RFC3339Nano format (e.g. 2006-01-02T15:04:05.999999999Z07:00)")
	f.Var(&c.to, "to", "search to timestamp in RFC3339Nano format (e.g. 2006-01-02T15:04:05.999999999Z07:00)")
	c.outputFlags.SetFlags(f)
	return c, nil
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init(&c.outputFlags)
	if err != nil {
		return err
	}
	defer cleanup()
	expr := "*"
	if len(args) > 0 {
		expr = strings.Join(args, " ")
	}
	req, err := parseExpr(c.PoolID, expr)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}
	req.Span = nano.NewSpanTs(nano.Ts(c.from), nano.Ts(c.to))
	params := map[string]string{"format": c.encoding}
	r, err := c.Conn.SearchRaw(ctx, *req, params)
	if err != nil {
		return fmt.Errorf("search error: %w", err)
	}

	defer r.Close()
	if c.debug {
		return c.runRawSearch(r)
	}
	writerOpts := c.outputFlags.Options()
	if c.encoding != "zng" {
		if writerOpts.Format != "zng" {
			return errors.New("-e cannot be used with -f")
		}
		return c.runRawSearch(r)
	}
	writer, err := c.outputFlags.Open(ctx, storage.NewLocalEngine())
	if err != nil {
		return err
	}
	stream := client.NewZngSearch(r)
	stream.SetOnCtrl(c.handleControl)
	if err := zio.Copy(writer, stream); err != nil {
		writer.Close()
		if ctx.Err() != nil {
			return errors.New("search aborted")
		}
		return err
	}
	return writer.Close()
}

// parseExpr creates an api.SearchRequest to be used with the client.
func parseExpr(pool ksuid.KSUID, expr string) (*api.SearchRequest, error) {
	search, err := compiler.ParseProc(expr)
	if err != nil {
		return nil, err
	}
	proc, err := json.Marshal(search)
	if err != nil {
		return nil, err
	}
	return &api.SearchRequest{
		Dir:  -1,
		Pool: pool,
		Proc: proc,
	}, nil
}

func (c *Command) handleControl(ctrl interface{}) {
	switch ctrl := ctrl.(type) {
	case *api.SearchStats:
		if c.stats {
			c.final = ctrl
		}
	case *api.SearchWarning:
		if c.warnings {
			for _, w := range ctrl.Warning {
				fmt.Fprintln(os.Stderr, w)
			}
		}
	}
}

func (c *Command) runRawSearch(r io.Reader) error {
	w := os.Stdout
	filename := c.outputFlags.FileName()
	if filename != "" {
		var err error
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		file, err := fs.OpenFile(filename, flags, 0600)
		if err != nil {
			return err
		}
		defer file.Close()
		w = file
	}
	_, err := io.Copy(w, r)
	return err
}

type tsflag nano.Ts

func (t tsflag) String() string {
	if t == 0 {
		return "min"
	}
	if nano.Ts(t) == nano.MaxTs {
		return "max"
	}
	return nano.Ts(t).Time().Format(time.RFC3339Nano)
}

func (t *tsflag) Set(val string) error {
	if val == "min" {
		*t = 0
		return nil
	}
	if val == "max" {
		*t = tsflag(nano.MaxTs)
		return nil
	}
	in, err := time.Parse(time.RFC3339Nano, val)
	if err != nil {
		return err
	}
	*t = tsflag(nano.TimeToTs(in))
	return nil
}