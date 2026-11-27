package ipynb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
)

// EncodeDocumentStream пишет согласованный ipynb в каноническом порядке по одной ячейке.
func EncodeDocumentStream(ctx context.Context, destination io.Writer, doc domain.Document, each func(func(domain.Cell) error) error) error {
	fields := header(doc)
	keys := make([]string, 0, len(fields)+1)
	for key := range fields {
		keys = append(keys, key)
	}
	keys = append(keys, "cells")
	slices.Sort(keys)
	writer := documentWriter{ctx: ctx, writer: destination, remaining: 128 << 20}
	if err := writer.write([]byte("{")); err != nil {
		return err
	}
	for i, key := range keys {
		if i > 0 {
			if err := writer.write([]byte(",")); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(key)
		if err != nil {
			return err
		}
		if err = writer.write(append(encoded, ':')); err != nil {
			return err
		}
		if key != "cells" {
			if err = writer.write(fields[key]); err != nil {
				return err
			}
			continue
		}
		if err = writer.write([]byte("[")); err != nil {
			return err
		}
		first := true
		if err = each(func(cell domain.Cell) error {
			if !first {
				if e := writer.write([]byte(",")); e != nil {
					return e
				}
			}
			first = false
			data, e := encodedCell(cell)
			if e != nil {
				return e
			}
			return writer.write(data)
		}); err != nil {
			return err
		}
		if err = writer.write([]byte("]")); err != nil {
			return err
		}
	}
	return writer.write([]byte("}"))
}

func encodedCell(c domain.Cell) ([]byte, error) {
	if (c.HasRuntimeMetadata || c.OutputTruncated) && RuntimeMetadataOverhead(c) < 0 {
		return nil, errors.New("runtime metadata namespace is occupied")
	}
	m := runtimeMetadata(c, light(c))
	if len(c.Outputs) > 0 {
		m["outputs"] = c.Outputs
	}
	if c.HasExecutionCount {
		put(m, "execution_count", c.ExecutionCount)
	}
	if len(c.Attachments) > 0 {
		m["attachments"] = c.Attachments
	}
	return json.Marshal(m)
}

type documentWriter struct {
	ctx       context.Context
	writer    io.Writer
	remaining int
}

func (w *documentWriter) write(data []byte) error {
	if len(data) > w.remaining {
		return domain.ErrTooLarge
	}
	for len(data) > 0 {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		part := data[:min(len(data), 64<<10)]
		n, err := w.writer.Write(part)
		if err != nil {
			return err
		}
		if n != len(part) {
			return io.ErrShortWrite
		}
		w.remaining -= n
		data = data[n:]
	}
	return nil
}

// EncodeDocument собирает полный документ только для внутренних callers.
func EncodeDocument(doc domain.Document, cells []domain.Cell) ([]byte, error) {
	var out bytes.Buffer
	err := EncodeDocumentStream(context.Background(), &out, doc, func(write func(domain.Cell) error) error {
		for _, cell := range cells {
			if err := write(cell); err != nil {
				return err
			}
		}
		return nil
	})
	return out.Bytes(), err
}
