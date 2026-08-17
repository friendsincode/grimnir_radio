/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"context"
	"fmt"
	"reflect"

	"gorm.io/gorm/schema"
)

// nullableUUIDSerializer maps an empty Go string to SQL NULL and back for
// optional `type:uuid` columns. Without it, gorm writes "" for an unset uuid
// field, which Postgres rejects (SQLSTATE 22P02) — so a media-less row (live DJ,
// webstream now-playing) fails to persist. The field stays a plain string;
// callers keep comparing against "" and never see a pointer or NULL.
//
// Register once and tag the column: `gorm:"type:uuid;serializer:nulluuid"`.
type nullableUUIDSerializer struct{}

func (nullableUUIDSerializer) Value(_ context.Context, _ *schema.Field, _ reflect.Value, fieldValue interface{}) (interface{}, error) {
	if s, _ := fieldValue.(string); s != "" {
		return s, nil
	}
	return nil, nil // empty -> SQL NULL
}

func (nullableUUIDSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue interface{}) error {
	var s string
	switch v := dbValue.(type) {
	case nil:
		s = ""
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("nulluuid: unsupported scan type %T", dbValue)
	}
	field.ReflectValueOf(ctx, dst).SetString(s)
	return nil
}

func init() {
	schema.RegisterSerializer("nulluuid", nullableUUIDSerializer{})
}
