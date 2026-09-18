package domain

import "time"

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
	ID        int64
	Title     string
	SubID     int64
	SubObj    Kind
	Satellite SubObject
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

type SubObject interface {
	isSatellite()
}

type Satellite struct {
	ID        int64
	MainID    int64
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

type Tool struct {
	Satellite

	Description1 *string
}

func (*Tool) isSatellite() {}

type Table struct {
	Satellite

	Description2 *string
}

func (*Table) isSatellite() {}

type Chair struct {
	Satellite

	Description3 *string
	Type         ChairType
}

func (*Chair) isSatellite() {}
