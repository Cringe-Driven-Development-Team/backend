package usecase

import (
	"testing"

	"go.uber.org/mock/gomock"
)

type notebookFixture struct {
	uc    *NotebookUsecase
	meta  *MockMetadata
	files *MockFiles
	codec *MockNotebookCodec
}

func newNotebookFixture(t *testing.T) notebookFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := notebookFixture{meta: NewMockMetadata(ctrl), files: NewMockFiles(ctrl), codec: NewMockNotebookCodec(ctrl)}
	f.uc = NewNotebookUsecase(f.meta, f.files, f.codec)
	f.uc.newID = func() string { return "fixed-id" }
	return f
}
