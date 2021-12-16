package del

import (
	"errors"
	"flag"
	"fmt"

	"github.com/brimdata/zed/cli"
	"github.com/brimdata/zed/cli/lakeflags"
	"github.com/brimdata/zed/cmd/zed/root"
	"github.com/brimdata/zed/lakeparse"
	"github.com/brimdata/zed/pkg/charm"
)

var Cmd = &charm.Spec{
	Name:  "delete",
	Usage: "delete id [id ...]",
	Short: "delete data objects from a pool branch",
	Long: `
The delete command takes a list of data object IDs and
deletes references to those object from HEAD by commiting a new
delete operation to HEAD.
Once the delete operation completes, the deleted data is no longer seen
when read data from the pool.

No data is actually removed from the lake.  Instead, a delete
operation is an action in the pool's commit journal.  Any delete
can be "undone" by adding the commits back to the log using
"zed revert".
`,
	New: New,
}

type Command struct {
	*root.Command
	cli.LakeFlags
	lakeFlags lakeflags.Flags
	cli.CommitFlags
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*root.Command)}
	c.CommitFlags.SetFlags(f)
	c.LakeFlags.SetFlags(f)
	c.lakeFlags.SetFlags(f)
	return c, nil
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init()
	if err != nil {
		return err
	}
	defer cleanup()
	lake, err := c.Open(ctx)
	if err != nil {
		return err
	}
	head, err := c.lakeFlags.HEAD()
	if err != nil {
		return err
	}
	poolName := head.Pool
	if poolName == "" {
		return lakeflags.ErrNoHEAD
	}
	poolID, err := lake.PoolID(ctx, poolName)
	if err != nil {
		return err
	}
	ids, err := lakeparse.ParseIDs(args)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return errors.New("no data object IDs specified")
	}
	commit, err := lake.Delete(ctx, poolID, head.Branch, ids, c.CommitMessage())
	if err != nil {
		return err
	}
	if !c.lakeFlags.Quiet {
		fmt.Printf("%s delete committed\n", commit)
	}
	return nil
}