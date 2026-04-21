package main

import (
	"errors"
	"flag"
)

func normalizeFlagParseError(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}
