package vng

import (
	"io"

	"github.com/brimdata/super"
	"github.com/brimdata/super/zcode"
	"golang.org/x/sync/errgroup"
)

type ArrayEncoder struct {
	typ     super.Type
	values  Encoder
	lengths *Int64Encoder
	count   uint32
}

var _ Encoder = (*ArrayEncoder)(nil)

func NewArrayEncoder(typ *super.TypeArray) *ArrayEncoder {
	return &ArrayEncoder{
		typ:     typ.Type,
		values:  NewEncoder(typ.Type),
		lengths: NewInt64Encoder(),
	}
}

func (a *ArrayEncoder) Write(body zcode.Bytes) {
	a.count++
	it := body.Iter()
	var len int64
	for !it.Done() {
		a.values.Write(it.Next())
		len++
	}
	a.lengths.Write(len)
}

func (a *ArrayEncoder) Encode(group *errgroup.Group) {
	a.lengths.Encode(group)
	a.values.Encode(group)
}

func (a *ArrayEncoder) Emit(w io.Writer) error {
	if err := a.lengths.Emit(w); err != nil {
		return err
	}
	return a.values.Emit(w)
}

func (a *ArrayEncoder) Metadata(off uint64) (uint64, Metadata) {
	off, lens := a.lengths.Metadata(off)
	off, vals := a.values.Metadata(off)
	return off, &Array{
		Length:  a.count,
		Lengths: lens.(*Primitive).Location, //XXX
		Values:  vals,
	}
}

type SetEncoder struct {
	ArrayEncoder
}

func NewSetEncoder(typ *super.TypeSet) *SetEncoder {
	return &SetEncoder{
		ArrayEncoder{
			typ:     typ.Type,
			values:  NewEncoder(typ.Type),
			lengths: NewInt64Encoder(),
		},
	}
}

func (s *SetEncoder) Metadata(off uint64) (uint64, Metadata) {
	off, meta := s.ArrayEncoder.Metadata(off)
	array := meta.(*Array)
	return off, &Set{
		Length:  array.Length,
		Lengths: array.Lengths,
		Values:  array.Values,
	}
}
