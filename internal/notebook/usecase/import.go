package usecase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
)

// ImportDecoder проверяет IPYNB и возвращает документ, ячейки и число пропусков.
// Ошибки валидации декодера должны быть пригодны для ответа клиенту.
type ImportDecoder func([]byte) (domain.Document, []domain.Cell, int, error)

// WithImportDecoder подключает декодер загружаемых документов.
func WithImportDecoder(decode ImportDecoder) Option {
	return func(uc *NotebookUsecase) { uc.decodeImport = decode }
}

// ImportStreamDecoder проверяет загруженный поток с отменой и обрабатывает по одной ячейке.
type ImportStreamDecoder func(context.Context, io.ReadSeeker) (domain.Document, []domain.Cell, int, error)

// WithImportStream подключает потоковое декодирование в HTTP приложении.
func WithImportStream(decode ImportStreamDecoder) Option {
	return func(uc *NotebookUsecase) { uc.decodeStream = decode }
}

// Import создаёт частный блокнот из готовых байтов для внутренних callers.
func (uc *NotebookUsecase) Import(ctx context.Context, ownerID int64, name string, raw []byte) (domain.Notebook, int, error) {
	return uc.importDecoded(ctx, ownerID, name, func() (domain.Document, []domain.Cell, int, error) {
		if uc.decodeImport == nil {
			return domain.Document{}, nil, 0, errors.New("notebook import decoder is unavailable")
		}
		return uc.decodeImport(raw)
	})
}

// ImportStream создаёт документ из ограниченного загруженного файла. Вход остаётся у HTTP caller.
func (uc *NotebookUsecase) ImportStream(ctx context.Context, ownerID int64, name string, input io.ReadSeeker) (domain.Notebook, int, error) {
	return uc.importDecoded(ctx, ownerID, name, func() (domain.Document, []domain.Cell, int, error) {
		if uc.decodeStream != nil {
			return uc.decodeStream(ctx, input)
		}
		if uc.decodeImport == nil {
			return domain.Document{}, nil, 0, errors.New("notebook import decoder is unavailable")
		}
		data, err := io.ReadAll(io.LimitReader(input, MaxNotebookBytes+1))
		if err != nil {
			return domain.Document{}, nil, 0, err
		}
		if len(data) > MaxNotebookBytes {
			return domain.Document{}, nil, 0, ErrTooLarge
		}
		return uc.decodeImport(data)
	})
}

// importDecoded валидирует имя и атомарно сохраняет только разобранный документ.
func (uc *NotebookUsecase) importDecoded(ctx context.Context, ownerID int64, name string, decode func() (domain.Document, []domain.Cell, int, error)) (domain.Notebook, int, error) {
	var empty domain.Notebook
	if utf8.RuneCountInString(name) > 128 {
		return empty, 0, ErrInvalidName
	}
	name, err := NormalizeName(name)
	if err != nil {
		return empty, 0, err
	}
	if err = ctx.Err(); err != nil {
		return empty, 0, err
	}
	doc, cells, skipped, err := decode()
	if err != nil {
		if ctx.Err() != nil {
			return empty, 0, ctx.Err()
		}
		return empty, 0, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}
	if err = ctx.Err(); err != nil {
		return empty, 0, err
	}
	n, err := uc.repo.Create(ctx, domain.Notebook{OwnerID: ownerID, Name: name, Document: doc, Cells: cells})
	if err != nil {
		return empty, 0, err
	}
	n.Access = domain.AccessFor(n.OwnerID, ownerID)
	return n, skipped, nil
}
