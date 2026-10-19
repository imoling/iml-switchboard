package core

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Call 是账本里的一行，字段名与 docs/API.md 一致。
type Call struct {
	ID              string    `json:"id"`
	TS              time.Time `json:"ts"`
	Source          string    `json:"source"`
	Host            string    `json:"host,omitempty"`
	SessionID       string    `json:"session_id,omitempty"`
	Kind            Kind      `json:"kind"`
	TaskType        string    `json:"task_type"`
	Tier            Tier      `json:"tier,omitempty"`
	MediaTier       string    `json:"media_tier,omitempty"`
	Lane            LaneID    `json:"lane"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	Reason          []string  `json:"reason"`
	Candidates      []Target  `json:"candidates"`
	FallbackFrom    string    `json:"fallback_from,omitempty"`
	InputTokens     int       `json:"input_tokens,omitempty"`
	OutputTokens    int       `json:"output_tokens,omitempty"`
	CacheReadTokens int       `json:"cache_read_tokens,omitempty"`
	Units           float64   `json:"units,omitempty"`
	Unit            string    `json:"unit,omitempty"`
	CostUSD         float64   `json:"cost_usd"`
	CostNative      float64   `json:"cost_native,omitempty"`
	Currency        string    `json:"currency,omitempty"`
	QuotaUnits      float64   `json:"quota_units,omitempty"`
	BaselineCostUSD float64   `json:"baseline_cost_usd"`
	UserEscalated   bool      `json:"user_escalated"`
	LatencyMS       int       `json:"latency_ms"`
	OK              bool      `json:"ok"`
	Error           string    `json:"error,omitempty"`
	PromptExcerpt   string    `json:"prompt_excerpt,omitempty"`
	ResultPath      string    `json:"result_path,omitempty"`
	JobID           string    `json:"job_id,omitempty"`
	ResultText      string    `json:"result_text,omitempty"`
	// 完整的提问和回答，只给评估用：重做和盲评要看到原话，摘录会截断。不进接口输出
	PromptFull string `json:"-"`
	ResultFull string `json:"-"`
	// 下面两个只在接口输出时填，不入库
	TaskName     string `json:"task_name,omitempty"`
	ModelDisplay string `json:"model_display,omitempty"`
}

// Ledger 是 SQLite 账本。
type Ledger struct{ db *sql.DB }

// OpenLedger 打开或创建账本。
func OpenLedger(path string) (*Ledger, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	schema := `
CREATE TABLE IF NOT EXISTS calls (
  id TEXT PRIMARY KEY, ts TEXT NOT NULL, source TEXT, host TEXT, session_id TEXT,
  kind TEXT, task_type TEXT, tier TEXT, media_tier TEXT, lane TEXT, provider TEXT, model TEXT,
  reason TEXT, candidates TEXT, fallback_from TEXT,
  input_tokens INTEGER DEFAULT 0, output_tokens INTEGER DEFAULT 0, cache_read_tokens INTEGER DEFAULT 0,
  units REAL DEFAULT 0, unit TEXT, cost_usd REAL DEFAULT 0, cost_native REAL DEFAULT 0, currency TEXT,
  quota_units REAL DEFAULT 0, baseline_cost_usd REAL DEFAULT 0, user_escalated INTEGER DEFAULT 0,
  latency_ms INTEGER DEFAULT 0, ok INTEGER DEFAULT 1, error TEXT, prompt_excerpt TEXT, result_path TEXT, job_id TEXT, result_text TEXT
);
CREATE INDEX IF NOT EXISTS calls_ts ON calls(ts);
CREATE INDEX IF NOT EXISTS calls_task ON calls(task_type, ts);
CREATE TABLE IF NOT EXISTS jobs (id TEXT PRIMARY KEY, kind TEXT, status TEXT, data TEXT, created_at TEXT, updated_at TEXT);
CREATE TABLE IF NOT EXISTS evaluations (id TEXT PRIMARY KEY, data TEXT, started_at TEXT);
CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, v TEXT);
`
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	// 后加的列：老账本补上，已有就跳过
	for _, col := range []string{"prompt_full", "result_full"} {
		if _, err := db.Exec(`ALTER TABLE calls ADD COLUMN ` + col + ` TEXT`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, err
		}
	}
	return &Ledger{db: db}, nil
}

// Close 关闭数据库。
func (l *Ledger) Close() error { return l.db.Close() }

// CleanExcerpts 把记录和媒体任务摘录里宿主注入的内容剥掉；给升级后清一次历史用，返回改了几条。
func (l *Ledger) CleanExcerpts(clean func(string) string) (int, error) {
	rows, err := l.db.Query(`SELECT id, prompt_excerpt, coalesce(prompt_full,'') FROM calls WHERE prompt_excerpt LIKE '%<system-reminder%' OR prompt_excerpt LIKE '%<session>%' OR prompt_excerpt LIKE '%<user_query>%'`)
	if err != nil {
		return 0, err
	}
	type row struct{ id, text, full string }
	var dirty []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.text, &r.full) == nil {
			dirty = append(dirty, r)
		}
	}
	rows.Close()
	n := 0
	for _, r := range dirty {
		// 摘录是截断过的，注入块可能没闭合；有完整原话就按原话重算
		src := r.text
		if r.full != "" {
			src = r.full
		}
		if c := excerpt(clean(src), 200); c != r.text {
			if _, err := l.db.Exec(`UPDATE calls SET prompt_excerpt=? WHERE id=?`, c, r.id); err == nil {
				n++
			}
		}
	}
	jobs, err := l.LoadJobs()
	if err != nil {
		return n, err
	}
	for _, j := range jobs {
		if c := clean(j.PromptExcerpt); c != j.PromptExcerpt {
			j.PromptExcerpt = c
			if l.SaveJob(j) == nil {
				n++
			}
		}
	}
	return n, nil
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

// Insert 写入一行。
func (l *Ledger) Insert(c *Call) error {
	_, err := l.db.Exec(`INSERT INTO calls (`+callCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.TS.UTC().Format(time.RFC3339Nano), c.Source, c.Host, c.SessionID, string(c.Kind), c.TaskType, string(c.Tier), c.MediaTier,
		string(c.Lane), c.Provider, c.Model, js(c.Reason), js(c.Candidates), c.FallbackFrom,
		c.InputTokens, c.OutputTokens, c.CacheReadTokens, c.Units, c.Unit, c.CostUSD, c.CostNative, c.Currency,
		c.QuotaUnits, c.BaselineCostUSD, boolInt(c.UserEscalated), c.LatencyMS, boolInt(c.OK), c.Error, c.PromptExcerpt, c.ResultPath, c.JobID, c.ResultText, c.PromptFull, c.ResultFull)
	return err
}

// UpdateResult 任务完成后回填产物与费用。
func (l *Ledger) UpdateResult(id string, path string, cost float64, ok bool, errText string) error {
	_, err := l.db.Exec(`UPDATE calls SET result_path=?, cost_usd=?, ok=?, error=? WHERE id=?`, path, cost, boolInt(ok), errText, id)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const callCols = `id,ts,source,host,session_id,kind,task_type,tier,media_tier,lane,provider,model,reason,candidates,fallback_from,input_tokens,output_tokens,cache_read_tokens,units,unit,cost_usd,cost_native,currency,quota_units,baseline_cost_usd,user_escalated,latency_ms,ok,error,prompt_excerpt,result_path,job_id,result_text,prompt_full,result_full`

func scanCall(r interface{ Scan(...any) error }) (*Call, error) {
	var c Call
	var ts, reason, cands string
	var esc, ok int
	var host, sess, tier, mtier, fb, unit, cur, errs, pe, rp, jid, rt, pf, rf sql.NullString
	err := r.Scan(&c.ID, &ts, &c.Source, &host, &sess, (*string)(&c.Kind), &c.TaskType, &tier, &mtier, (*string)(&c.Lane), &c.Provider, &c.Model, &reason, &cands, &fb,
		&c.InputTokens, &c.OutputTokens, &c.CacheReadTokens, &c.Units, &unit, &c.CostUSD, &c.CostNative, &cur, &c.QuotaUnits, &c.BaselineCostUSD, &esc, &c.LatencyMS, &ok, &errs, &pe, &rp, &jid, &rt, &pf, &rf)
	if err != nil {
		return nil, err
	}
	c.TS, _ = time.Parse(time.RFC3339Nano, ts)
	c.Host, c.SessionID, c.Tier, c.MediaTier, c.FallbackFrom, c.Unit, c.Currency = host.String, sess.String, Tier(tier.String), mtier.String, fb.String, unit.String, cur.String
	c.Error, c.PromptExcerpt, c.ResultPath, c.JobID, c.ResultText = errs.String, pe.String, rp.String, jid.String, rt.String
	c.PromptFull, c.ResultFull = pf.String, rf.String
	c.UserEscalated, c.OK = esc == 1, ok == 1
	_ = json.Unmarshal([]byte(reason), &c.Reason)
	_ = json.Unmarshal([]byte(cands), &c.Candidates)
	if c.Reason == nil {
		c.Reason = []string{}
	}
	if c.Candidates == nil {
		c.Candidates = []Target{}
	}
	return &c, nil
}

// Get 取一行。
func (l *Ledger) Get(id string) (*Call, error) {
	return scanCall(l.db.QueryRow(`SELECT `+callCols+` FROM calls WHERE id=?`, id))
}

// ListFilter 是请求记录的筛选条件。
type ListFilter struct {
	Source, Lane, Tier, Query, Kind, Host string
	Before                                time.Time
	Limit                                 int
	Since                                 time.Time
	OnlyOK                                bool
}

// List 按时间倒序列出。
func (l *Ledger) List(f ListFilter) ([]*Call, error) {
	where, args := []string{"1=1"}, []any{}
	add := func(cond string, v any) { where = append(where, cond); args = append(args, v) }
	if f.Source != "" {
		add("source=?", f.Source)
	}
	if f.Lane != "" {
		if f.Lane == "sub" {
			where = append(where, "lane LIKE '%_sub'")
		} else {
			add("lane=?", f.Lane)
		}
	}
	if f.Tier != "" {
		add("tier=?", f.Tier)
	}
	if f.Kind != "" {
		add("kind=?", f.Kind)
	}
	if f.Host != "" {
		add("host=?", f.Host)
	}
	if f.Query != "" {
		q := "%" + f.Query + "%"
		where = append(where, "(prompt_excerpt LIKE ? OR model LIKE ? OR task_type LIKE ?)")
		args = append(args, q, q, q)
	}
	if !f.Before.IsZero() {
		add("ts<?", f.Before.UTC().Format(time.RFC3339Nano))
	}
	if !f.Since.IsZero() {
		add("ts>=?", f.Since.UTC().Format(time.RFC3339Nano))
	}
	if f.OnlyOK {
		where = append(where, "ok=1")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := l.db.Query(`SELECT `+callCols+` FROM calls WHERE `+strings.Join(where, " AND ")+` ORDER BY ts DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Call{}
	for rows.Next() {
		c, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Escalate 标记「判低了」，返回该任务类型近 7 天的标记数。
func (l *Ledger) Escalate(id string) (taskType string, n int, err error) {
	if _, err = l.db.Exec(`UPDATE calls SET user_escalated=1 WHERE id=?`, id); err != nil {
		return
	}
	if err = l.db.QueryRow(`SELECT task_type FROM calls WHERE id=?`, id).Scan(&taskType); err != nil {
		return
	}
	since := time.Now().Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	err = l.db.QueryRow(`SELECT COUNT(*) FROM calls WHERE task_type=? AND user_escalated=1 AND ts>=?`, taskType, since).Scan(&n)
	return
}

// Overview 是概览页的聚合结果。
type Overview struct {
	Calls           int            `json:"calls"`
	APICostUSD      float64        `json:"api_cost_usd"`
	BaselineCostUSD float64        `json:"baseline_cost_usd"`
	SavedUSD        float64        `json:"saved_usd"`
	SavedPct        float64        `json:"saved_pct"`
	EscalationRate  float64        `json:"escalation_rate"`
	EscalationHot   string         `json:"escalation_hot_task,omitempty"`
	TierDist        map[string]int `json:"tier_dist"`
	LaneDist        map[string]int `json:"lane_dist"`
	Daily           []DailyPoint   `json:"daily"`
	TopTasks        []TopTask      `json:"top_tasks"`
	PrevCalls       int            `json:"prev_calls"`
	MonthAPICostUSD float64        `json:"month_api_cost_usd"`
}

// DailyPoint 是每天的花费与基线。
type DailyPoint struct {
	Date            string  `json:"date"`
	CostUSD         float64 `json:"cost_usd"`
	BaselineCostUSD float64 `json:"baseline_cost_usd"`
}

// TopTask 是花得最多的任务类型。
type TopTask struct {
	TaskType string  `json:"task_type"`
	Lane     string  `json:"lane"`
	Count    int     `json:"count"`
	CostUSD  float64 `json:"cost_usd"`
}

// Aggregate 计算概览。days=1 表示今天。
func (l *Ledger) Aggregate(days int, loc *time.Location) (*Overview, error) {
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	prevStart := start.AddDate(0, 0, -days)
	calls, err := l.List(ListFilter{Since: prevStart, Limit: 100000})
	if err != nil {
		return nil, err
	}
	o := &Overview{TierDist: map[string]int{}, LaneDist: map[string]int{}, Daily: []DailyPoint{}}
	daily := map[string]*DailyPoint{}
	// 只看今天时按小时分桶，键形如 2006-01-02T15；多天按天
	bucketFmt := "2006-01-02"
	if days == 1 {
		bucketFmt = "2006-01-02T15"
		for h := 0; h < 24; h++ {
			key := start.Add(time.Duration(h) * time.Hour).Format(bucketFmt)
			daily[key] = &DailyPoint{Date: key}
			o.Daily = append(o.Daily, DailyPoint{Date: key})
		}
	} else {
		for d := 0; d < days; d++ {
			day := start.AddDate(0, 0, d).Format(bucketFmt)
			daily[day] = &DailyPoint{Date: day}
			o.Daily = append(o.Daily, DailyPoint{Date: day})
		}
	}
	tasks := map[string]*TopTask{}
	esc := 0
	escBy := map[string]int{}
	for _, c := range calls {
		if c.TS.Before(start) {
			o.PrevCalls++
			continue
		}
		o.Calls++
		o.APICostUSD += c.CostUSD
		o.BaselineCostUSD += c.BaselineCostUSD
		if c.UserEscalated {
			esc++
			escBy[c.TaskType]++
		}
		if c.Kind == KindText {
			o.TierDist[string(c.Tier)]++
		} else {
			o.TierDist["media"]++
		}
		switch {
		case c.Lane == LaneAPI:
			o.LaneDist["api"]++
		case c.Lane == LaneMedia:
			o.LaneDist["media"]++
		default:
			o.LaneDist["sub"]++
		}
		day := c.TS.In(loc).Format(bucketFmt)
		if p, ok := daily[day]; ok {
			p.CostUSD += c.CostUSD
			p.BaselineCostUSD += c.BaselineCostUSD
		}
		t := tasks[c.TaskType]
		if t == nil {
			t = &TopTask{TaskType: c.TaskType, Lane: laneGroup(c.Lane)}
			tasks[c.TaskType] = t
		}
		t.Count++
		t.CostUSD += c.CostUSD
	}
	for i := range o.Daily {
		o.Daily[i] = *daily[o.Daily[i].Date]
	}
	o.SavedUSD = o.BaselineCostUSD - o.APICostUSD
	if o.BaselineCostUSD > 0 {
		o.SavedPct = o.SavedUSD / o.BaselineCostUSD * 100
	}
	if o.Calls > 0 {
		o.EscalationRate = float64(esc) / float64(o.Calls) * 100
	}
	best := 0
	for k, v := range escBy {
		if v > best {
			best, o.EscalationHot = v, k
		}
	}
	for _, t := range tasks {
		o.TopTasks = append(o.TopTasks, *t)
	}
	// 按「代价」排：费用优先，其次次数
	for i := 0; i < len(o.TopTasks); i++ {
		for j := i + 1; j < len(o.TopTasks); j++ {
			a, b := o.TopTasks[i], o.TopTasks[j]
			if b.CostUSD > a.CostUSD || (b.CostUSD == a.CostUSD && b.Count > a.Count) {
				o.TopTasks[i], o.TopTasks[j] = b, a
			}
		}
	}
	if len(o.TopTasks) > 5 {
		o.TopTasks = o.TopTasks[:5]
	}
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	_ = l.db.QueryRow(`SELECT COALESCE(SUM(cost_usd),0) FROM calls WHERE ts>=?`, monthStart.UTC().Format(time.RFC3339Nano)).Scan(&o.MonthAPICostUSD)
	return o, nil
}

func laneGroup(l LaneID) string {
	switch l {
	case LaneAPI:
		return "api"
	case LaneMedia:
		return "media"
	}
	return "sub"
}

// MonthCostByAccount 返回本月各 provider 的花费（USD 与原币）。
func (l *Ledger) MonthCostByAccount(loc *time.Location) (map[string][2]float64, error) {
	now := time.Now().In(loc)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	rows, err := l.db.Query(`SELECT provider, COALESCE(SUM(cost_usd),0), COALESCE(SUM(cost_native),0) FROM calls WHERE ts>=? GROUP BY provider`, monthStart.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][2]float64{}
	for rows.Next() {
		var p string
		var usd, nat float64
		if err := rows.Scan(&p, &usd, &nat); err != nil {
			return nil, err
		}
		out[p] = [2]float64{usd, nat}
	}
	return out, nil
}

// QuotaWindowUsage 返回某渠道在时间窗内的额度占用之和与次数。
func (l *Ledger) QuotaWindowUsage(lane LaneID, since time.Time, model string) (units float64, n int, err error) {
	q := `SELECT COALESCE(SUM(quota_units),0), COUNT(*) FROM calls WHERE lane=? AND ts>=?`
	args := []any{string(lane), since.UTC().Format(time.RFC3339Nano)}
	if model != "" {
		q += ` AND model=?`
		args = append(args, model)
	}
	err = l.db.QueryRow(q, args...).Scan(&units, &n)
	return
}

// EscalationStats 返回近 N 天每个任务类型的判低次数、总次数、平均费用。
type EscalationStat struct {
	TaskType    string
	Escalations int
	Total       int
	AvgCostUSD  float64
	Tier        string
}

// EscalationStats 聚合判低数据，用于规则建议。
func (l *Ledger) EscalationStats(days int) ([]EscalationStat, error) {
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	rows, err := l.db.Query(`SELECT task_type, SUM(user_escalated), COUNT(*), AVG(cost_usd), MAX(tier) FROM calls WHERE ts>=? AND kind='text' GROUP BY task_type`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EscalationStat
	for rows.Next() {
		var s EscalationStat
		var tier sql.NullString
		if err := rows.Scan(&s.TaskType, &s.Escalations, &s.Total, &s.AvgCostUSD, &tier); err != nil {
			return nil, err
		}
		s.Tier = tier.String
		out = append(out, s)
	}
	return out, nil
}

// --- jobs 持久化 ---

// SaveJob 写入或更新任务。
func (l *Ledger) SaveJob(j *Job) error {
	_, err := l.db.Exec(`INSERT INTO jobs(id,kind,status,data,created_at,updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, data=excluded.data, updated_at=excluded.updated_at`,
		j.ID, string(j.Kind), j.Status, js(j), j.CreatedAt.UTC().Format(time.RFC3339Nano), j.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// LoadJobs 读出全部任务（启动时恢复）。
func (l *Ledger) LoadJobs() ([]*Job, error) {
	rows, err := l.db.Query(`SELECT data FROM jobs ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		var j Job
		if json.Unmarshal([]byte(d), &j) == nil {
			out = append(out, &j)
		}
	}
	return out, nil
}

// KV 读写小状态（粘性会话、冷却等）。
func (l *Ledger) KVSet(k string, v any) error {
	_, err := l.db.Exec(`INSERT INTO kv(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, js(v))
	return err
}

// KVGet 读 kv。
func (l *Ledger) KVGet(k string, into any) bool {
	var v string
	if err := l.db.QueryRow(`SELECT v FROM kv WHERE k=?`, k).Scan(&v); err != nil {
		return false
	}
	return json.Unmarshal([]byte(v), into) == nil
}

// SaveEvaluation / ListEvaluations 存评估结果。
func (l *Ledger) SaveEvaluation(id string, started time.Time, data any) error {
	_, err := l.db.Exec(`INSERT INTO evaluations(id,data,started_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, id, js(data), started.UTC().Format(time.RFC3339Nano))
	return err
}

// ListEvaluations 返回原始 JSON 列表，最新在前。
func (l *Ledger) ListEvaluations() ([]json.RawMessage, error) {
	rows, err := l.db.Query(`SELECT data FROM evaluations ORDER BY started_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(d))
	}
	return out, nil
}

// Sample 抽样近 N 天某些档位的成功文本请求，用于评估。
func (l *Ledger) Sample(days, n int, tiers []string) ([]*Call, error) {
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	ph := strings.TrimRight(strings.Repeat("?,", len(tiers)), ",")
	args := []any{since}
	for _, t := range tiers {
		args = append(args, t)
	}
	args = append(args, n)
	// 只抽能完整重现的：新记录要有完整的提问和回答；老记录只有摘录，摘录没被截断、也不是多轮对话才算
	rows, err := l.db.Query(`SELECT `+callCols+` FROM calls WHERE ts>=? AND kind='text' AND ok=1 AND tier IN (`+ph+`)
  AND source NOT IN ('evaluation','classifier')
  AND ((COALESCE(prompt_full,'')<>'' AND COALESCE(result_full,'')<>'')
    OR (prompt_full IS NULL AND prompt_excerpt<>'' AND prompt_excerpt NOT LIKE '%…' AND COALESCE(result_text,'')<>'' AND result_text NOT LIKE '%…' AND source<>'facade'))
  ORDER BY RANDOM() LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Call
	for rows.Next() {
		c, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// String 便于日志。
func (c *Call) String() string {
	return fmt.Sprintf("%s %s/%s %s %s $%.4f", c.ID, c.Lane, c.Model, c.TaskType, c.Tier, c.CostUSD)
}

// DeleteJob 删任务记录；文件不动。
func (l *Ledger) DeleteJob(id string) error {
	_, err := l.db.Exec(`DELETE FROM jobs WHERE id=?`, id)
	return err
}

// DeleteCall 删一条请求记录；文件不动。
func (l *Ledger) DeleteCall(id string) error {
	_, err := l.db.Exec(`DELETE FROM calls WHERE id=?`, id)
	return err
}

// HostActivity 是一个宿主近期派活的情况。
type HostActivity struct {
	Calls int       `json:"calls"`
	Last  time.Time `json:"last"`
}

// HostActivity 按宿主统计 since 以来的请求数和最近一次时间；评估、判档、界面里的试一句不算。
func (l *Ledger) HostActivity(since time.Time) (map[string]HostActivity, error) {
	rows, err := l.db.Query(`SELECT host, COUNT(*), MAX(ts) FROM calls WHERE ts>=? AND host<>'' AND source NOT IN ('evaluation','try','quick','cli') GROUP BY host`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]HostActivity{}
	for rows.Next() {
		var h, last string
		var n int
		if err := rows.Scan(&h, &n, &last); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339Nano, last)
		out[h] = HostActivity{Calls: n, Last: t}
	}
	return out, rows.Err()
}

// MonthCost 是本月按量付费已花的美元。每次执行前查预算用它：一条求和，不把当天的记录全读出来。
func (l *Ledger) MonthCost(loc *time.Location) (float64, error) {
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	var v float64
	err := l.db.QueryRow(`SELECT COALESCE(SUM(cost_usd),0) FROM calls WHERE ts>=?`, start.UTC().Format(time.RFC3339Nano)).Scan(&v)
	return v, err
}

// PruneFull 清掉 before 之前为评估存的完整原话，摘录和费用照留。置成空串而不是 NULL：NULL 会被当成老记录参与抽样。
func (l *Ledger) PruneFull(before time.Time) (int64, error) {
	res, err := l.db.Exec(`UPDATE calls SET prompt_full='', result_full='' WHERE ts<? AND (COALESCE(prompt_full,'')<>'' OR COALESCE(result_full,'')<>'')`, before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
