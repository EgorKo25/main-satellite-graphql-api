package domain

import (
	"encoding/json"
	"time"
)

type Kind string

const (
	Tools  Kind = "tools"
	Tables Kind = "tables"
	Chairs Kind = "chairs"
)

type ChairType string

const (
	ABC ChairType = "abc"
	CDE ChairType = "cde"
)

type Main struct {
	ID            int64
	Title         string
	SubID         int64
	SubObj        Kind
	SatelliteData json.RawMessage `db:"-"`
	CreatedAt     time.Time
	UpdatedAt     time.Time `db:"update_at"`
	DeletedAt     *time.Time
}

type SubObject interface {
	Kind() Kind
}

type Satellite struct {
	ID        int64      `json:"id"`
	MainID    int64      `json:"main_id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"update_at"`
	DeletedAt *time.Time `json:"deleted_at"`
}

type Tool struct {
	Satellite

	Description1 *string `json:"description1"`
}

func (*Tool) Kind() Kind { return Tools }

type Table struct {
	Satellite

	Description2 *string `json:"description2"`
}

func (*Table) Kind() Kind { return Tables }

type Chair struct {
	Satellite

	Description3 *string   `json:"description3"`
	Type         ChairType `json:"type"`
}

func (*Chair) Kind() Kind { return Chairs }
