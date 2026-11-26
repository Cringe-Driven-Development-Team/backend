package delivery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
)

// ImportNotebook проверяет загрузку до вызова операции сохранения.
func (h *Handler) ImportNotebook(ctx context.Context, req api.ImportNotebookRequestObject) (api.ImportNotebookResponseObject, error) {
	var result domain.Notebook
	var skipped int
	var name string
	var err error
	var importFile func() (domain.Notebook, int, error)
	if stream, ok := h.uc.(interface {
		ImportStream(context.Context, int64, string, io.ReadSeeker) (domain.Notebook, int, error)
	}); ok {
		file, createErr := os.CreateTemp("", "cellestial-ipynb-*")
		if createErr != nil {
			return nil, createErr
		}
		defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
		name, err = readImportedParts(req.Body, file)
		importFile = func() (domain.Notebook, int, error) {
			if _, e := file.Seek(0, io.SeekStart); e != nil {
				return domain.Notebook{}, 0, e
			}
			return stream.ImportStream(ctx, userID(ctx), name, file)
		}
	} else {
		var file []byte
		name, file, err = readImportedNotebook(req.Body)
		importFile = func() (domain.Notebook, int, error) { return h.uc.Import(ctx, userID(ctx), name, file) }
	}
	var limit *http.MaxBytesError
	// multipart.Reader может завершиться до эпилога HTTP-тела.
	if body := middleware.NotebookImportBody(ctx); body != nil {
		_, drainErr := io.Copy(io.Discard, body)
		if err == nil || errors.As(drainErr, &limit) {
			err = drainErr
		}
	}
	if err != nil {
		if errors.As(err, &limit) || errors.Is(err, usecase.ErrTooLarge) {
			return importTooLarge(), nil
		}
		return api.ImportNotebook400JSONResponse{Code: api.ErrorCodeValidationError, Message: "expected one name and one nonempty file in a complete multipart body"}, nil
	}
	result, skipped, err = importFile()
	switch {
	case errors.Is(err, usecase.ErrTooLarge):
		return importTooLarge(), nil
	case errors.Is(err, usecase.ErrInvalidDocument), errors.Is(err, usecase.ErrEmptyName), errors.Is(err, usecase.ErrInvalidName):
		return api.ImportNotebook400JSONResponse{Code: api.ErrorCodeValidationError, Message: err.Error()}, nil
	case err != nil:
		return nil, err
	}
	return api.ImportNotebook201JSONResponse{Notebook: toAPINotebook(result), SkippedRawCells: skipped}, nil
}

// importTooLarge формирует общий ответ для лимитов файла, документа и тела.
func importTooLarge() api.ImportNotebook413JSONResponse {
	return api.ImportNotebook413JSONResponse{Code: api.ErrorCodeValidationError, Message: "notebook import exceeds size limit"}
}

// readImportedNotebook сохраняет совместимый путь для вызывающих, принимающих байты.
func readImportedNotebook(reader *multipart.Reader) (string, []byte, error) {
	var buffer bytes.Buffer
	name, err := readImportedParts(reader, &buffer)
	return name, buffer.Bytes(), err
}

// readImportedParts принимает ровно одну часть name и один файл в любом порядке.
func readImportedParts(reader *multipart.Reader, destination io.Writer) (string, error) {
	invalid := errors.New("invalid notebook multipart")
	if reader == nil {
		return "", invalid
	}
	var name []byte
	var size int64
	hasFile := false
	for {
		part, err := reader.NextRawPart()
		if err == io.EOF { //nolint:errorlint // Только прямой EOF означает прочитанную завершающую границу.
			break
		}
		if err != nil {
			return "", err
		}
		switch {
		case part.FormName() == "name" && name == nil && part.FileName() == "":
			const maxNameBytes = 128 * 4
			name, err = io.ReadAll(io.LimitReader(part, maxNameBytes+1))
			if len(name) > maxNameBytes {
				return "", invalid
			}
		case part.FormName() == "file" && !hasFile && part.FileName() != "":
			hasFile = true
			size, err = io.Copy(destination, io.LimitReader(part, usecase.MaxNotebookBytes+1))
			if size > usecase.MaxNotebookBytes {
				return "", usecase.ErrTooLarge
			}
		default:
			return "", invalid
		}
		if err != nil {
			return "", err
		}
	}
	if name == nil || size == 0 {
		return "", invalid
	}
	return string(name), nil
}
