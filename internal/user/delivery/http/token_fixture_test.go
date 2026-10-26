package delivery

import (
	"testing"

	"go.uber.org/mock/gomock"
)

func testTokens(t *testing.T) *MockTokenParser {
	t.Helper()
	tokens := NewMockTokenParser(gomock.NewController(t))
	// Routing helpers also serve anonymous requests; token parsing is optional here.
	tokens.EXPECT().Parse("access-1").Return(int64(1), nil).AnyTimes()
	tokens.EXPECT().Parse("access-42").Return(int64(42), nil).AnyTimes()
	return tokens
}
