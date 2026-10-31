package model

import (
	"time"

	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

type CollisionType string

const (
	CollisionWithCar         CollisionType = "with other car"
	CollisionWithEnvironment CollisionType = "with environment"
)

type Collision struct {
	ID              string         `json:"ID"`
	Type            CollisionType  `json:"Type"`
	Time            time.Time      `json:"Time" ts:"date"`
	OtherDriverGUID udp.DriverGUID `json:"OtherDriverGUID"`
	OtherDriverName string         `json:"OtherDriverName"`
	Speed           float64        `json:"Speed"`
}

type LiveTimingsPersistedData struct {
	SessionType udp.SessionType
	Track       string
	TrackLayout string
	SessionName string

	Drivers map[udp.DriverGUID]*RaceControlDriver
}
