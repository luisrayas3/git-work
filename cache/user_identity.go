package cache

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/repository"
)

// UserIdentitySource says how EnsureUserIdentity arrived at the user identity.
type UserIdentitySource int

const (
	// UserIdentityConfigured means git-bug.identity was already set.
	UserIdentityConfigured UserIdentitySource = iota
	// UserIdentityAdopted means an existing identity
	// matching git's user.email was adopted and set.
	UserIdentityAdopted
	// UserIdentityCreated means a new identity
	// was created from git's user.name/user.email and set.
	UserIdentityCreated
)

// EnsureUserIdentity returns the user identity,
// bootstrapping one from git configuration when none is set:
// an existing identity with the same email (case-insensitive) is adopted,
// otherwise a new one is created from user.name/user.email,
// and either way it is recorded as git-bug.identity.
//
// Nobody should have to run `user new` before their first mutation;
// `user new` and `user adopt` remain available as overrides.
func (c *RepoCache) EnsureUserIdentity() (*IdentityCache, UserIdentitySource, error) {
	i, err := c.GetUserIdentity()
	if err == nil {
		return i, UserIdentityConfigured, nil
	}
	if !errors.Is(err, identity.ErrNoIdentitySet) {
		return nil, UserIdentityConfigured, err
	}

	name, email, err := c.gitUser()
	if err != nil {
		return nil, UserIdentityConfigured, err
	}

	matches, err := c.identitiesByEmail(email)
	if err != nil {
		return nil, UserIdentityConfigured, err
	}
	if len(matches) > 1 {
		// Prefer an exact name match before giving up.
		var named []*IdentityCache
		for _, m := range matches {
			if m.Name() == name {
				named = append(named, m)
			}
		}
		if len(named) == 1 {
			matches = named
		} else {
			ids := make([]string, len(matches))
			for k, m := range matches {
				ids[k] = fmt.Sprintf("%s (%s)", m.Id().Human(), m.DisplayName())
			}
			return nil, UserIdentityConfigured, fmt.Errorf(
				"several identities use %s: %s; pick one with `user adopt <id>`",
				email, strings.Join(ids, ", "))
		}
	}

	source := UserIdentityAdopted
	if len(matches) == 1 {
		i = matches[0]
	} else {
		i, err = c.identities.New(name, email)
		if err != nil {
			return nil, UserIdentityConfigured, err
		}
		source = UserIdentityCreated
	}

	if err := c.SetUserIdentity(i); err != nil {
		return nil, UserIdentityConfigured, err
	}
	return i, source, nil
}

// gitUser reads user.name and user.email from git,
// turning a missing or empty value into an actionable error.
func (c *RepoCache) gitUser() (name, email string, err error) {
	name, err = c.repo.GetUserName()
	if err != nil && !errors.Is(err, repository.ErrNoConfigEntry) {
		return "", "", err
	}
	email, err = c.repo.GetUserEmail()
	if err != nil && !errors.Is(err, repository.ErrNoConfigEntry) {
		return "", "", err
	}
	if name == "" || email == "" {
		return "", "", errors.New("no identity is set and git has no user.name/user.email to create one from;\n" +
			"set them with `git config --global user.name`/`user.email`, or run `user new`")
	}
	return name, email, nil
}

// identitiesByEmail loads every identity whose email matches, ignoring case.
// Emails are not part of the excerpt, so this reads each identity;
// stores hold few enough of them for that to be cheap.
func (c *RepoCache) identitiesByEmail(email string) ([]*IdentityCache, error) {
	var matches []*IdentityCache
	for _, id := range c.identities.AllIds() {
		i, err := c.identities.Resolve(id)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(i.Email(), email) {
			matches = append(matches, i)
		}
	}
	return matches, nil
}
