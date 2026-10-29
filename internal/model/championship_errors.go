package model

import (
	"errors"
)

var (
	ErrInvalidChampionshipEvent = errors.New("servermanager: invalid championship event")
	ErrInvalidChampionshipClass = errors.New("servermanager: invalid championship class")
)
