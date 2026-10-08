package graph

import (
	"fmt"

	"github.com/samber/lo"
	"github.com/vektah/gqlparser/v2/ast"
)

func validateVariable(schema *ast.Schema, typ *ast.Type, value any) error {
	if value == nil {
		return nil
	}

	definition := schema.Types[typ.NamedType]
	if definition == nil {
		return nil
	}

	switch definition.Kind {
	case ast.Enum:
		name, ok := value.(string)
		if !ok || definition.EnumValues.ForName(name) == nil {
			return fmt.Errorf("invalid value for enum %s", definition.Name)
		}
	case ast.InputObject:
		fields, ok := value.(map[string]any)
		if !ok {
			return nil
		}

		if definition.Directives.ForName("oneOf") != nil {
			_, hasNull := lo.FindKey(fields, nil)
			if len(fields) != 1 || hasNull {
				return fmt.Errorf("%s requires exactly one supplied, non-null field", definition.Name)
			}
		}

		for name, item := range fields {
			field := definition.Fields.ForName(name)
			if field == nil {
				return fmt.Errorf("unknown field %s in %s", name, definition.Name)
			}

			if err := validateVariable(schema, field.Type, item); err != nil {
				return err
			}
		}
	}

	return nil
}
