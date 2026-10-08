package benchmarks

import "github.com/EgorKo25/main-satellite-graphql-api/internal/domain"

//go:generate go tool easyjson -all -no_std_marshalers -output_filename models_easyjson.go models.go

//easyjson:json
type Tool domain.Tool

//easyjson:json
type Table domain.Table

//easyjson:json
type Chair domain.Chair
