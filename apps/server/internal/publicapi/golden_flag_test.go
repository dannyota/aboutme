package publicapi

import "flag"

var updateGolden = flag.Bool("update", false, "rewrite this package's golden files from the current code")
