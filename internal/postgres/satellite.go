package postgres

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

func exactlyOne(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return err
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("expected one changed row, got %d", tag.RowsAffected())
	}

	return nil
}
