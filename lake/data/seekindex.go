package data

import (
	"context"
	"fmt"

	"github.com/brimdata/zed"
	"github.com/brimdata/zed/lake/seekindex"
	"github.com/brimdata/zed/pkg/storage"
	"github.com/brimdata/zed/runtime/sam/expr"
	"github.com/brimdata/zed/vector"
	"github.com/brimdata/zed/zio/zngio"
	"github.com/brimdata/zed/zson"
)

func LookupSeekRange(ctx context.Context, engine storage.Engine, path *storage.URI,
	obj *Object, pruner expr.Evaluator) ([]seekindex.Range, error) {
	if pruner == nil {
		// scan whole object
		return nil, nil
	}
	r, err := engine.Get(ctx, obj.SeekIndexURI(path))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var ranges seekindex.Ranges
	zctx := zed.NewContext()
	arena := zed.NewArena()
	defer arena.Unref()
	unmarshaler := zson.NewZNGUnmarshaler()
	unmarshaler.SetContext(zctx, arena)
	reader := zngio.NewReader(zctx, r)
	defer reader.Close()
	ectx := expr.NewContext(arena)
	for {
		val, err := reader.Read()
		if val == nil || err != nil {
			return ranges, err
		}
		result := pruner.Eval(ectx, *val)
		if result.Type() == zed.TypeBool && result.Bool() {
			continue
		}
		var entry seekindex.Entry
		if err := unmarshaler.Unmarshal(*val, &entry); err != nil {
			return nil, fmt.Errorf("corrupt seek index entry for %q at value: %q (%w)", obj.ID.String(), zson.String(val), err)
		}
		ranges.Append(entry)
	}
}

func RangeFromBitVector(ctx context.Context, engine storage.Engine, path *storage.URI,
	o *Object, b *vector.Bool) ([]seekindex.Range, error) {
	arena := zed.NewArena()
	defer arena.Unref()
	index, err := readSeekIndex(ctx, arena, engine, path, o)
	if err != nil {
		return nil, err
	}
	return index.Filter(b), nil
}

func readSeekIndex(ctx context.Context, arena *zed.Arena, engine storage.Engine, path *storage.URI, o *Object) (seekindex.Index, error) {
	r, err := engine.Get(ctx, o.SeekIndexURI(path))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	zctx := zed.NewContext()
	zr := zngio.NewReader(zctx, r)
	u := zson.NewZNGUnmarshaler()
	u.SetContext(zctx, arena)
	var index seekindex.Index
	for {
		val, err := zr.Read()
		if val == nil {
			return index, err
		}
		var entry seekindex.Entry
		if err := u.Unmarshal(*val, &entry); err != nil {
			return nil, err
		}
		index = append(index, entry)
	}
}
