package ipynb

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
)

// DecodeImportStream разбирает IPYNB по полям и ячейкам, без копии исходного документа целиком.
func DecodeImportStream(ctx context.Context, input io.ReadSeeker) (domain.Document, []domain.Cell, int, error) {
	return decodeImportStream(ctx, input, 128<<20)
}

func decodeImportStream(ctx context.Context, input io.ReadSeeker, maxBytes int) (domain.Document, []domain.Cell, int, error) {
	var doc domain.Document
	size, err := input.Seek(0, io.SeekEnd)
	if err != nil {
		return doc, nil, 0, err
	}
	if size > int64(maxBytes) {
		return doc, nil, 0, domain.ErrTooLarge
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		return doc, nil, 0, err
	}
	if err = validateStreamUnicode(ctx, input); err != nil {
		return doc, nil, 0, err
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		return doc, nil, 0, err
	}
	decoder := json.NewDecoder(&importContextReader{ctx: ctx, reader: input})
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return doc, nil, 0, errors.New("expected IPYNB object")
	}
	fields := map[string]json.RawMessage{}
	seen := map[string]bool{}
	var cells []domain.Cell
	var sizes []int64
	skipped := 0
	remaining := maxBytes
	for decoder.More() {
		item, tokenErr := decoder.Token()
		key, ok := item.(string)
		if tokenErr != nil || !ok || seen[key] || strings.ContainsRune(key, 0) {
			return doc, nil, 0, errors.New("invalid or duplicate IPYNB field")
		}
		seen[key] = true
		if key == "cells" {
			cells, sizes, skipped, err = decodeImportCells(decoder, &remaining)
			if err != nil {
				return doc, nil, 0, err
			}
			continue
		}
		var raw json.RawMessage
		if err = decoder.Decode(&raw); err != nil {
			return doc, nil, 0, err
		}
		normalized, canonicalErr := CanonicalJSONLimit(raw, remaining)
		if canonicalErr != nil {
			return doc, nil, 0, canonicalErr
		}
		remaining -= len(normalized)
		if key == "nbformat" || key == "nbformat_minor" {
			fields[key] = raw
		} else {
			fields[key] = normalized
		}
	}
	if _, err = decoder.Token(); err != nil {
		return doc, nil, 0, err
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return doc, nil, 0, errors.New("trailing IPYNB data")
	}
	if string(take(fields, "nbformat")) != "4" {
		return doc, nil, 0, errors.New("expected nbformat 4")
	}
	minor, err := strconv.Atoi(string(take(fields, "nbformat_minor")))
	if err != nil || minor < 0 {
		return doc, nil, 0, errors.New("nbformat_minor must be a non-negative integer")
	}
	metadata := take(fields, "metadata")
	if len(metadata) == 0 || metadata[0] != '{' {
		return doc, nil, 0, errors.New("metadata must be an object")
	}
	if !seen["cells"] {
		return doc, nil, 0, errors.New("cells must be an array")
	}
	doc = domain.Document{NBFormat: 4, NBFormatMinor: minor, Metadata: metadata, Extra: fields}
	if DocumentSize(HeaderSize(doc), sizes) > int64(maxBytes) {
		return domain.Document{}, nil, 0, domain.ErrTooLarge
	}
	return doc, cells, skipped, nil
}

func decodeImportCells(decoder *json.Decoder, remaining *int) ([]domain.Cell, []int64, int, error) {
	value, err := decoder.Token()
	if err != nil || value != json.Delim('[') {
		return nil, nil, 0, errors.New("cells must be an array")
	}
	cells := make([]domain.Cell, 0)
	seen := map[string]bool{}
	var skipped int
	for index := 0; decoder.More(); index++ {
		if index >= 1000 {
			return nil, nil, 0, domain.ErrTooLarge
		}
		var raw json.RawMessage
		if err = decoder.Decode(&raw); err != nil {
			return nil, nil, 0, err
		}
		item, decodeErr := decodeJSONValue(raw)
		if decodeErr != nil {
			return nil, nil, 0, decodeErr
		}
		fields, ok := item.(map[string]any)
		if !ok {
			return nil, nil, 0, fmt.Errorf("cells[%d]: expected cell object", index)
		}
		kind, _ := fields["cell_type"].(string)
		if kind == "raw" {
			budget := math.MaxInt
			if err = canonicalBudget(fields, &budget); err != nil {
				return nil, nil, 0, fmt.Errorf("cells[%d]: %w", index, err)
			}
			skipped++
			continue
		}
		if kind != "code" && kind != "markdown" {
			return nil, nil, 0, fmt.Errorf("cells[%d]: %w", index, ErrUnknownCellType)
		}
		var sourceValue source
		if err = sourceValue.decode(fields["source"]); err != nil {
			return nil, nil, 0, fmt.Errorf("cells[%d]: %w", index, err)
		}
		if len(sourceValue) > 64<<10 {
			return nil, nil, 0, domain.ErrTooLarge
		}
		fields["source"] = string(sourceValue)
		if count, ok := fields["execution_count"].(json.Number); ok {
			if _, err = count.Int64(); err != nil {
				return nil, nil, 0, fmt.Errorf("cells[%d]: execution_count must be an int64 or null", index)
			}
		}
		normalized, canonicalErr := canonicalValue(fields, *remaining)
		if canonicalErr != nil {
			return nil, nil, 0, fmt.Errorf("cells[%d]: %w", index, canonicalErr)
		}
		cell, cellErr := decodeCell(normalized, seen, true)
		if cellErr != nil {
			return nil, nil, 0, fmt.Errorf("cells[%d]: %w", index, cellErr)
		}
		*remaining -= len(normalized)
		cells = append(cells, cell)
	}
	if _, err = decoder.Token(); err != nil {
		return nil, nil, 0, err
	}
	if err = assignImportIDs(cells, seen); err != nil {
		return nil, nil, 0, err
	}
	sizes := make([]int64, len(cells))
	for i, cell := range cells {
		sizes[i] = CellSize(cell)
	}
	return cells, sizes, skipped, nil
}

// importContextReader связывает буферизацию и парсинг с отменой запроса.
type importContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *importContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p[:min(len(p), 64<<10)])
}

// validateStreamUnicode отвергает невалидный UTF-8 и непарные escape-последовательности до json.Decoder.
func validateStreamUnicode(ctx context.Context, input io.Reader) error {
	reader := bufio.NewReaderSize(&importContextReader{ctx: ctx, reader: input}, 64<<10)
	inString := false
	for {
		r, size, err := reader.ReadRune()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if r == utf8.RuneError && size == 1 {
			return errors.New("invalid UTF-8")
		}
		switch {
		case r == '"':
			inString = !inString
		case r == '\\' && inString:
			escape, err := reader.ReadByte()
			if err != nil {
				return err
			}
			if escape != 'u' {
				continue
			}
			high, err := readEscape(reader)
			if err != nil {
				return err
			}
			if high >= 0xDC00 && high <= 0xDFFF {
				return errors.New("unpaired surrogate")
			}
			if high >= 0xD800 && high < 0xDC00 {
				var prefix [2]byte
				if _, err = io.ReadFull(reader, prefix[:]); err != nil || prefix != [2]byte{'\\', 'u'} {
					return errors.New("unpaired surrogate")
				}
				low, err := readEscape(reader)
				if err != nil || low < 0xDC00 || low > 0xDFFF {
					return errors.New("unpaired surrogate")
				}
			}
		}
	}
}

func readEscape(r io.Reader) (uint64, error) {
	var digits [4]byte
	if _, err := io.ReadFull(r, digits[:]); err != nil {
		return 0, err
	}
	return strconv.ParseUint(string(digits[:]), 16, 16)
}
