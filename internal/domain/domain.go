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
	Kind() Kind
	Metadata() *Satellite
}

type Satellite struct {
	ID        int64
	MainID    int64
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

func (satellite *Satellite) Metadata() *Satellite { return satellite }

type Tool struct {
	Satellite

	Description1 *string
}

func (*Tool) Kind() Kind { return Tools }

type Table struct {
	Satellite

	Description2 *string
}

func (*Table) Kind() Kind { return Tables }

type Chair struct {
	Satellite

	Description3 *string
	Type         ChairType
}

func (*Chair) Kind() Kind { return Chairs }
