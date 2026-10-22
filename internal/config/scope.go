package config

import "path/filepath"

type Level int

const (
	// LevelNone is never cached: a write, a login, a vote.
	LevelNone Level = iota
	LevelGlobal
	LevelLeague
	LevelAccount
	LevelMember
)

func (l Level) String() string {
	switch l {
	case LevelGlobal:
		return "global"
	case LevelLeague:
		return "league"
	case LevelAccount:
		return "account"
	case LevelMember:
		return "member"
	}
	return "none"
}

// Tags name cached responses and say whose they are. Other packages invalidate by tag, and a
// tag that matches nothing fails silently — it drops no files and reports success — so the set
// is declared once and checked against.
var Tags = map[string]Level{
	"players": LevelGlobal, "player": LevelGlobal, "mv": LevelGlobal, "teams": LevelGlobal,
	"week": LevelGlobal, "calendar": LevelGlobal, "formations": LevelGlobal, "rewards": LevelGlobal,
	"ff_market": LevelGlobal, "ff_detail": LevelGlobal, "ff_player": LevelGlobal,
	"ff_absences": LevelGlobal, "ff_hierarchy": LevelGlobal,

	"squad": LevelLeague, "standing": LevelLeague, "activity": LevelLeague,
	"lineup": LevelLeague,

	"me": LevelAccount, "leagues": LevelAccount,

	// The market carries our own bids and offers inline, so it is not the league's.
	"market": LevelMember, "money": LevelMember, "offers": LevelMember,
	"favourites": LevelMember, "own_lineup": LevelMember, "reward": LevelMember,
	"necro-state": LevelMember, "necro-shields": LevelMember,
}

func TagLevel(tag string) Level { return Tags[tag] }

// CacheScope is the folder one level's answers live in. Only a Profile or GlobalScope makes
// one, so a request cannot name somebody else's folder by hand.
type CacheScope struct {
	level   Level
	account string
	league  string
	dir     string
	// legacy is where the answer lived before scopes, read only to replay a frozen cache.
	legacy string
}

func (s CacheScope) Level() Level    { return s.level }
func (s CacheScope) Account() string { return s.account }
func (s CacheScope) League() string  { return s.league }
func (s CacheScope) Dir() string     { return s.dir }
func (s CacheScope) Legacy() string  { return s.legacy }

func GlobalCacheDir() string { return filepath.Join(CacheRoot, "global") }

func GlobalScope() CacheScope {
	return CacheScope{level: LevelGlobal, dir: GlobalCacheDir()}
}

// Scope is this profile's folder at one level.
//
//	<cache root>/global                              LevelGlobal
//	<cache root>/leagues/<league>                    LevelLeague
//	<cache root>/accounts/<account>                  LevelAccount
//	<cache root>/accounts/<account>/leagues/<league> LevelMember, the profile's CacheDir
func (p Profile) Scope(level Level) CacheScope {
	scope := CacheScope{level: level, account: p.Account, league: p.League, dir: p.CacheDir}
	switch level {
	case LevelNone:
		return CacheScope{}
	case LevelGlobal:
		return CacheScope{level: LevelGlobal, dir: filepath.Join(p.cacheRoot, "global")}
	case LevelLeague:
		scope.account, scope.legacy = "", p.CacheDir
		if p.League != "" {
			scope.dir = filepath.Join(p.cacheRoot, "leagues", p.League)
		}
	case LevelAccount:
		scope.league, scope.legacy = "", p.CacheDir
		if p.Account != "" {
			scope.dir = filepath.Join(p.cacheRoot, "accounts", p.Account)
		}
	}
	return scope
}

func (p Profile) ScopeOf(tag string) CacheScope { return p.Scope(TagLevel(tag)) }
