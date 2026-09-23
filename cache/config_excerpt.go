package cache

import (
	"encoding/gob"
	"time"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/util/lamport"
)

// Package initialisation used to register the type for (de)serialization
func init() {
	gob.Register(ConfigExcerpt{})
}

var _ Excerpt = &ConfigExcerpt{}

// ConfigExcerpt holds a config entity's whole document.
//
// Attributes is carried whole, as IssueExcerpt carries Fields:
// a config entity is a handful of small values,
// and carrying them means listing, exporting and picking a duplicate's winner
// never load an entity (E8).
// The create lamport time is what E7 resolves duplicate keys by.
type ConfigExcerpt struct {
	id entity.Id

	CreateLamportTime lamport.Time
	EditLamportTime   lamport.Time
	CreateUnixTime    int64
	EditUnixTime      int64

	AuthorId   entity.Id
	Shape      config.Shape
	Key        string
	Archived   bool
	Attributes map[string]config.Value

	CreateMetadata map[string]string
}

func NewConfigExcerpt(c *ConfigCache) *ConfigExcerpt {
	snap := c.Snapshot()

	attributes := make(map[string]config.Value, len(snap.Attributes))
	for name, value := range snap.Attributes {
		attributes[name] = value
	}

	return &ConfigExcerpt{
		id:                c.Id(),
		CreateLamportTime: c.CreateLamportTime(),
		EditLamportTime:   c.EditLamportTime(),
		CreateUnixTime:    snap.Operations[0].Time().Unix(),
		EditUnixTime:      snap.EditTime().Unix(),
		AuthorId:          snap.Author.Id(),
		Shape:             snap.Shape,
		Key:               snap.Key,
		Archived:          snap.Archived,
		Attributes:        attributes,
		CreateMetadata:    snap.Operations[0].AllMetadata(),
	}
}

func (e *ConfigExcerpt) setId(id entity.Id) {
	e.id = id
}

func (e *ConfigExcerpt) Id() entity.Id {
	return e.id
}

func (e *ConfigExcerpt) CreateTime() time.Time {
	return time.Unix(e.CreateUnixTime, 0)
}

func (e *ConfigExcerpt) EditTime() time.Time {
	return time.Unix(e.EditUnixTime, 0)
}

// Attribute returns one attribute, and whether it is set.
func (e *ConfigExcerpt) Attribute(name string) (config.Value, bool) {
	v, ok := e.Attributes[name]
	return v, ok
}

// AttributeString returns an attribute decoded as a string, and whether it is one.
func (e *ConfigExcerpt) AttributeString(name string) (string, bool) {
	v, ok := e.Attributes[name]
	if !ok {
		return "", false
	}
	return config.String(v)
}

/*
 * Sorting
 */

// ConfigsByCreation orders by (create lamport time, id),
// which is the order E7 picks a duplicate key's winner by:
// the first definition of a key wins,
// and a tie is broken by something every clone agrees on.
type ConfigsByCreation []*ConfigExcerpt

func (c ConfigsByCreation) Len() int { return len(c) }

func (c ConfigsByCreation) Less(i, j int) bool {
	if c[i].CreateLamportTime != c[j].CreateLamportTime {
		return c[i].CreateLamportTime < c[j].CreateLamportTime
	}
	return c[i].id < c[j].id
}

func (c ConfigsByCreation) Swap(i, j int) { c[i], c[j] = c[j], c[i] }

// ConfigsByKey orders by (shape, key, create lamport time, id),
// so that a listing is stable whatever order the refs came back in.
type ConfigsByKey []*ConfigExcerpt

func (c ConfigsByKey) Len() int { return len(c) }

func (c ConfigsByKey) Less(i, j int) bool {
	if c[i].Shape != c[j].Shape {
		return c[i].Shape < c[j].Shape
	}
	if c[i].Key != c[j].Key {
		return c[i].Key < c[j].Key
	}
	if c[i].CreateLamportTime != c[j].CreateLamportTime {
		return c[i].CreateLamportTime < c[j].CreateLamportTime
	}
	return c[i].id < c[j].id
}

func (c ConfigsByKey) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
