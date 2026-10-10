package core

// UpdateConfig reads the config file, applies fn and writes it back, so a
// change made by one front end keeps whatever another changed meanwhile.
func UpdateConfig(p Paths, fn func(*Config)) error {
	cfg, err := LoadConfig(p)
	if err != nil {
		return err
	}
	fn(&cfg)
	return SaveConfig(p, cfg)
}

// SetFavoriteTeams replaces the sport's favorite team abbreviations.
func (c *Config) SetFavoriteTeams(sport string, teams []string) {
	if c.FavoriteTeams == nil {
		c.FavoriteTeams = map[string][]string{}
	}
	if teams == nil {
		teams = []string{}
	}
	c.FavoriteTeams[sport] = teams
}

// SetInterval sets the poll interval in seconds, raised to MinInterval.
func (c *Config) SetInterval(seconds int) {
	c.Interval = max(seconds, MinInterval)
}
