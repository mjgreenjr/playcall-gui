package core

import "encoding/json"

// favoriteGroupsKey holds the GUI's remembered conference or division per
// sport: {"cfb": "Big Ten"}. It lives in Config.Extra, so it is only written
// once something is chosen and a fresh config file is unchanged.
const favoriteGroupsKey = "favorite_groups"

// FavoriteGroup is the group name remembered for sport, "" for none (the
// whole scoreboard). A malformed value counts as none.
func (c Config) FavoriteGroup(sport string) string {
	raw, ok := c.Extra[favoriteGroupsKey]
	if !ok {
		return ""
	}
	var m map[string]string
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return m[sport]
}

// SetFavoriteGroup remembers name for sport; "" forgets it. The key is
// dropped again when nothing is left in it.
func (c *Config) SetFavoriteGroup(sport, name string) {
	m := map[string]string{}
	if raw, ok := c.Extra[favoriteGroupsKey]; ok {
		_ = json.Unmarshal(raw, &m) // a malformed value is replaced
		if m == nil {
			m = map[string]string{}
		}
	}
	if name == "" {
		delete(m, sport)
	} else {
		m[sport] = name
	}
	if len(m) == 0 {
		delete(c.Extra, favoriteGroupsKey)
		return
	}
	b, _ := json.Marshal(m) // map keys come out sorted
	if c.Extra == nil {
		c.Extra = map[string]json.RawMessage{}
	}
	c.Extra[favoriteGroupsKey] = b
}

// SaveFavoriteGroup remembers a group in the config file. It reads the file
// again first, so a favorite team added elsewhere in the meantime is kept.
func SaveFavoriteGroup(p Paths, sport, name string) error {
	cfg, err := LoadConfig(p)
	if err != nil {
		return err
	}
	cfg.SetFavoriteGroup(sport, name)
	return SaveConfig(p, cfg)
}
