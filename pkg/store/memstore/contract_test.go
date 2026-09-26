package memstore

import (
	"testing"

	"github.com/kairn-io/kairn/pkg/store/storetest"
)

func TestContract(t *testing.T) {
	storetest.Run(t, New(), storetest.Options{Rollback: false})
}
