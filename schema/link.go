package schema

// LinksKey is the word the views fold every link of an issue into: the
// `links` row on show, and the name in a show spec's `expand` that stands for
// one side table per link relation with entries (doc/design/link-fields.md,
// L3, L4). It is reserved, so no field and no inverse may take it.
const LinksKey = "links"

// LinkRelation is one relation a type's issues are linked by: a stored link
// field of the type, or the inverse of a link field some type stores and
// which may point at this one.
type LinkRelation struct {
	// Name is the stored field's key, or the inverse's name.
	Name string
	// Stored says the relation is a field of the shown type; false, it is
	// read off the other issues, which hold it in Sources.
	Stored bool
	// Field is the stored field, for a stored relation.
	Field *Field
	// Sources are, for an inverse, the types holding the stored field and
	// that field: one key per type, since an inverse is unique within a type.
	Sources map[string]*Field
}

// Links lists the link relations of one type: its stored link fields in
// schema order, then the inverses of the link fields that may point at it,
// in the order their first source appears in the schema.
//
// The inverse of a link is a link when its stored side is: it has no entity
// of its own to mark (link-fields.md, L1).
func (s *Schema) Links(typeKey string) []LinkRelation {
	if s == nil {
		return nil
	}
	var out []LinkRelation
	if t, ok := s.Type(typeKey); ok {
		for _, key := range t.FieldKeys() {
			field := t.Fields[key]
			if field.Link && field.Kind.IsRelation() {
				out = append(out, LinkRelation{Name: key, Stored: true, Field: field})
			}
		}
	}

	at := map[string]int{}
	for _, sourceType := range s.TypeKeys() {
		t := s.Types[sourceType]
		for _, key := range t.FieldKeys() {
			field := t.Fields[key]
			if !field.Link || !field.Kind.IsRelation() || field.Inverse == "" {
				continue
			}
			if len(field.TargetTypes) > 0 && !containsString(field.TargetTypes, typeKey) {
				continue
			}
			i, seen := at[field.Inverse]
			if !seen {
				i = len(out)
				at[field.Inverse] = i
				out = append(out, LinkRelation{Name: field.Inverse, Sources: map[string]*Field{}})
			}
			out[i].Sources[sourceType] = field
		}
	}
	return out
}

// HasLinks says some field of the schema is a link.
func (s *Schema) HasLinks() bool {
	if s == nil {
		return false
	}
	for _, t := range s.Types {
		for _, field := range t.Fields {
			if field.Link {
				return true
			}
		}
	}
	return false
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
