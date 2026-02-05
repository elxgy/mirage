package uprobe

import "fmt"

var ErrNotSupported = fmt.Errorf("uprobes not supported on this platform")
