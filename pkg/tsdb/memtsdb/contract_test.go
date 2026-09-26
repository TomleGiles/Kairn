package memtsdb

import (
	"testing"
	"time"

	"github.com/kairn-io/kairn/pkg/tsdb/tsdbtest"
)

func TestContract(t *testing.T) { tsdbtest.Run(t, New(), time.Now()) }
