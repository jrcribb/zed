package vng

import (
	"bytes"
	"fmt"
	"io"

	"github.com/brimdata/zed"
	"github.com/brimdata/zed/zio"
	"github.com/brimdata/zed/zio/zngio"
	"github.com/brimdata/zed/zson"
)

// Writer implements the zio.Writer interface. A Writer creates a vector
// VNG object from a stream of zed.Records.
type Writer struct {
	zctx    *zed.Context
	writer  io.WriteCloser
	variant *VariantEncoder
}

var _ zio.Writer = (*Writer)(nil)

func NewWriter(w io.WriteCloser) *Writer {
	zctx := zed.NewContext()
	return &Writer{
		zctx:    zctx,
		writer:  w,
		variant: NewVariantEncoder(zctx),
	}
}

func (w *Writer) Close() error {
	firstErr := w.finalize()
	if err := w.writer.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (w *Writer) Write(val zed.Value) error {
	return w.variant.Write(val)
}

func (w *Writer) finalize() error {
	meta, dataSize, err := w.variant.Encode()
	if err != nil {
		return fmt.Errorf("system error: could not encode VNG metadata: %w", err)
	}
	// At this point all the vector data has been written out
	// to the underlying spiller, so we start writing zng at this point.
	var metaBuf bytes.Buffer
	zw := zngio.NewWriter(zio.NopCloser(&metaBuf))
	// First, we write the root segmap of the vector of integer type IDs.
	m := zson.NewZNGMarshalerWithContext(w.zctx)
	m.Decorate(zson.StyleSimple)
	arena := zed.NewArena()
	defer arena.Unref()
	val, err := m.Marshal(arena, meta)
	if err != nil {
		return fmt.Errorf("system error: could not marshal VNG metadata: %w", err)
	}
	if err := zw.Write(val); err != nil {
		return fmt.Errorf("system error: could not serialize VNG metadata: %w", err)
	}
	zw.EndStream()
	metaSize := zw.Position()
	// Header
	if _, err := w.writer.Write(Header{Version, uint32(metaSize), uint32(dataSize)}.Serialize()); err != nil {
		return fmt.Errorf("system error: could not write VNG header: %w", err)
	}
	// Metadata section
	if _, err := w.writer.Write(metaBuf.Bytes()); err != nil {
		return fmt.Errorf("system error: could not write VNG metadata section: %w", err)
	}
	// Data section
	if err := w.variant.Emit(w.writer); err != nil {
		return fmt.Errorf("system error: could not write VNG data section: %w", err)
	}
	return nil
}
