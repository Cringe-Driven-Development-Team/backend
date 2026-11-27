package ipynb

import (
	"bytes"
	"context"
	"errors"
	"regexp"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/google/uuid"
)

var importCellID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// DecodeImport проверяет байты IPYNB, возвращает документ, сохраняемые ячейки
// и число пропущенных raw-ячеек. Превышение лимита даёт domain.ErrTooLarge.
func DecodeImport(raw []byte) (domain.Document, []domain.Cell, int, error) {
	return decodeImport(raw, 128*1024*1024)
}

// decodeImport применяет общий бюджет maxBytes к входу и результату.
func decodeImport(raw []byte, maxBytes int) (domain.Document, []domain.Cell, int, error) {
	return decodeImportStream(context.Background(), bytes.NewReader(raw), maxBytes)
}

// assignImportIDs заполняет отсутствующие ID после проверки всех переданных ID.
func assignImportIDs(cells []domain.Cell, seen map[string]bool) error {
	for i := range cells {
		for cells[i].ID == "" {
			id, err := uuid.NewRandom()
			if err != nil {
				return errors.New("generate cell id")
			}
			value := id.String()
			if !seen[value] {
				cells[i].ID = value
				seen[value] = true
			}
		}
	}
	return nil
}
