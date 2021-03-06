package types

import (
	"container/list"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gogames/go_tetris/tetris"
	"github.com/gogames/go_tetris/timer"
)

var (
	ErrExisted  = fmt.Errorf("the table is already exist")
	ErrNotExist = fmt.Errorf("找不到该桌子.")
	ErrRoomFull = fmt.Errorf("桌子已满, 无法加入游戏.")
)

type sortedList struct {
	l       *list.List
	mu      sync.RWMutex
	tableId []int
}

func newSortList() *sortedList {
	return &sortedList{
		l:       list.New(),
		tableId: make([]int, 0),
	}
}

func (sl *sortedList) GetAll() []int {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.tableId
}

func (sl *sortedList) Get(index int) int {
	if index < 0 {
		return -1
	}
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	for e := sl.l.Front(); e != nil; e = e.Next() {
		if index == 0 {
			return e.Value.(int)
		}
		index--
	}
	return -1
}

func (sl *sortedList) Set(index, val int) {
	if index < 0 {
		return
	}
	sl.mu.Lock()
	defer sl.mu.Unlock()
	for e := sl.l.Front(); e != nil; e = e.Next() {
		if index == 0 {
			e.Value = val
		}
		index--
	}
}

func (sl *sortedList) Len() int {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.l.Len()
}

func (sl *sortedList) Less(i, j int) bool { return sl.Get(i) < sl.Get(j) }

func (sl *sortedList) Swap(i, j int) {
	iVal, jVal := sl.Get(i), sl.Get(j)
	sl.Set(i, jVal)
	sl.Set(j, iVal)
}

// add new table id
func (sl *sortedList) Add(i int) {
	defer func() {
		sl.Sort()
	}()
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.l.PushFront(i)
}

func (sl *sortedList) Delete(val int) {
	defer func() {
		sl.Sort()
	}()
	sl.mu.Lock()
	defer sl.mu.Unlock()
	for e := sl.l.Front(); e != nil; e = e.Next() {
		if e.Value.(int) == val {
			sl.l.Remove(e)
			return
		}
	}
}

func (sl *sortedList) Sort() {
	sort.Sort(sl)
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.tableId = make([]int, 0)
	for e := sl.l.Front(); e != nil; e = e.Next() {
		sl.tableId = append(sl.tableId, e.Value.(int))
	}
}

type Tables struct {
	Tables        map[int]*Table
	sortedTableId *sortedList
	mu            sync.RWMutex
	expires       map[int]*Table
}

func NewTables() *Tables {
	ts := &Tables{
		sortedTableId: newSortList(),
		Tables:        make(map[int]*Table),
		expires:       make(map[int]*Table),
	}
	return ts.init()
}

func (ts *Tables) init() *Tables {
	go findExpires(ts)
	return ts
}

func (ts *Tables) String() string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	str := "Currently we have the following tables:\n"
	for tid, t := range ts.Tables {
		str += fmt.Sprintf("table id: %d -> status: %s\n", tid, t.tStat)
	}
	return str
}

func findExpires(ts *Tables) {
	for {
		func() {
			ts.mu.Lock()
			defer ts.mu.Unlock()
			for tid, t := range ts.Tables {
				if t.Expire() {
					ts.expires[tid] = t
				}
			}
		}()
		time.Sleep(5 * time.Second)
	}
}

func (ts *Tables) GetExpireTables() map[int]*Table {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.expires
}

func (ts *Tables) ReleaseExpireTable(tid int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.Tables, tid)
	delete(ts.expires, tid)
	ts.sortedTableId.Delete(tid)
}

const defaultTableInPage = 9

// for hprose
func (ts *Tables) Wrap(numOfTableInPage, pageNum int, filterWait bool) []map[string]interface{} {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	tableIds := ts.sortedTableId.GetAll()
	if filterWait {
		tmpTableIds := make([]int, 0)
		for _, tid := range tableIds {
			if !ts.Tables[tid].IsStart() {
				tmpTableIds = append(tmpTableIds, tid)
			}
		}
		tableIds = tmpTableIds
	}
	l := len(tableIds)
	if l == 0 {
		return nil
	}
	if numOfTableInPage <= 0 {
		numOfTableInPage = defaultTableInPage
	}
	if pageNum <= 0 {
		pageNum = 1
	}
	res := make([]map[string]interface{}, 0)
	start, end := (pageNum-1)*numOfTableInPage, pageNum*numOfTableInPage-1
	switch l = l - 1; {
	case l <= start:
		end = l
		start = 0
		for start < end {
			start += numOfTableInPage
		}
		start -= numOfTableInPage
	case l <= end:
		end = l
	}
	if start < 0 {
		start = 0
	}
	for _, index := range tableIds[start : end+1] {
		res = append(res, ts.Tables[index].WrapTable())
	}
	return res
}

func (ts Tables) MarshalJSON() ([]byte, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	var res = make(map[string]*Table)
	for id, t := range ts.Tables {
		res[fmt.Sprintf("%d", id)] = t
	}
	return json.Marshal(res)
}

// get a Table
func (ts *Tables) GetTableById(id int) *Table {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.Tables[id]
}

// delete the Table
func (ts *Tables) DelTable(id int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.Tables, id)
	ts.sortedTableId.Delete(id)
}

// create a new Table
func (ts *Tables) NewTable(id int, title, host string, bet int) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if _, ok := ts.Tables[id]; ok {
		return ErrExisted
	}
	ts.Tables[id] = newTable(id, title, host, bet)
	ts.sortedTableId.Add(id)
	return nil
}

// join a Table
func (ts *Tables) JoinTable(id int, u *User, isOb bool) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	t, ok := ts.Tables[id]
	if !ok {
		return ErrNotExist
	}
	if isOb {
		t.JoinOB(u)
		return nil
	}
	return t.Join(u)
}

// number of tables
func (ts *Tables) Length() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.Tables)
}

const (
	statWaiting = "等待"
	statInGame  = "已开始"
)

const (
	zoneHeight            = 20
	zoneWidth             = 10
	defaultNumOfNextPiece = 5
	defaultInterval       = 1000
)

type gameOverStatus int

const (
	_ gameOverStatus = iota
	GameoverNormal
	Gameover1pQuit
	Gameover2pQuit
)

// table
type Table struct {
	mu sync.Mutex
	// basic table information
	tId    int
	tTitle string
	tStat  string
	tBet   int
	tHost  string
	// observers
	obs *obs
	// player 1p, 2p
	_1p, _2p *User
	// game 1p, 2p
	g1p, g2p *tetris.Game
	// 1p 2p ready ?
	ready1p, ready2p bool
	startTime        int64
	// timer
	timer               *timer.Timer
	remainedSeconds     int
	RemainedSecondsChan chan int
	// game over
	GameoverChan chan gameOverStatus
}

func newTable(id int, title, host string, bet int) *Table {
	return &Table{
		tId:                 id,
		tTitle:              title,
		tStat:               statWaiting,
		tBet:                bet,
		tHost:               host,
		obs:                 NewObs(),
		startTime:           time.Now().Unix(),
		remainedSeconds:     120,
		timer:               timer.NewTimer(1000),
		RemainedSecondsChan: make(chan int, 1<<3),
		GameoverChan:        make(chan gameOverStatus, 1<<3),
	}
}

func (t *Table) UpdateTimer() {
	for {
		if t.timer.IsPaused() {
			return
		}
		t.timer.Wait()
		if b := func() bool {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.remainedSeconds--
			if t.remainedSeconds <= 0 {
				t.GameoverChan <- GameoverNormal
				return true
			}
			t.RemainedSecondsChan <- t.remainedSeconds
			return false
		}(); b {
			return
		}
	}
}

func (t *Table) WrapTable() map[string]interface{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	return map[string]interface{}{
		"table_bet":      t.tBet,
		"table_id":       t.tId,
		"table_host":     t.tHost,
		"table_status":   t.tStat,
		"table_title":    t.tTitle,
		"table_1p":       t._1p,
		"table_2p":       t._2p,
		"table_1p_ready": t.ready1p,
		"table_2p_ready": t.ready2p,
		"table_obs":      t.obs.Wrap(),
	}
}

// table json
// func (t *Table) MarshalJSON() ([]byte, error) {
// 	t.mu.Lock()
// 	defer t.mu.Unlock()
// 	return json.Marshal(map[string]interface{}{
// 		"info":      t.tableInfo,
// 		"observers": t.obs,
// 		"1p":        t._1p,
// 		"2p":        t._2p,
// 		"1p_ready":  t.ready1p,
// 		"2p_ready":  t.ready2p,
// 	})
// }

// start the game, only used on game server
func (t *Table) StartGame() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.g1p, _ = tetris.NewGame(zoneHeight, zoneWidth, defaultNumOfNextPiece, defaultInterval)
	t.g2p, _ = tetris.NewGame(zoneHeight, zoneWidth, defaultNumOfNextPiece, defaultInterval)
	t.timer.Start()
	t.g1p.Start()
	t.g2p.Start()
	t.startTime = time.Now().Unix()
	t.tStat = statInGame
}

// stop the game, only used on game server
func (t *Table) StopGame() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.timer.Pause()
	t.timer.Reset()
	t.g1p.Stop()
	t.g2p.Stop()
	t.tStat = statWaiting
	t.startTime = time.Now().Unix()
}

// reset the table
func (t *Table) ResetTable() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.g1p = nil
	t.g2p = nil
	t.ready1p = false
	t.ready2p = false
	t.remainedSeconds = 120
	t.tStat = statWaiting
}

// set ready
func (t *Table) SwitchReady(uid int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if uid < 0 {
		return
	}
	switch uid {
	case t._1p.GetUid():
		t.ready1p = !t.ready1p
	case t._2p.GetUid():
		t.ready2p = !t.ready2p
	}
}

// should the table start
func (t *Table) ShouldStart() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ready1p && t.ready2p
}

const (
	maxNoPlayerDurationInSecs = 10
	maxGameDurationInSecs     = 300
	maxIdleDurationInSecs     = 3600
)

var MaxDurationOfTable = maxGameDurationInSecs + maxIdleDurationInSecs

func (t *Table) GetHost() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tHost
}

func (t *Table) GetIp() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Split(t.tHost, ":")[0]
}

func (t *Table) GetTid() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tId
}

// check if the table should expire
func (t *Table) Expire() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	tDur := time.Now().Unix() - t.startTime
	// if the game is start and have been played for longer than 300 seconds
	// or if the game is not start for 3600 seconds -> 1 hour
	// if the table has no players for 10 seconds, release it
	// there should be some network errors occur
	// so we have to manually release the table otherwise the users are not able to join game any more
	if t._1p == nil && t._2p == nil {
		return tDur > maxNoPlayerDurationInSecs
	}
	if t.tStat == statInGame {
		return tDur > maxGameDurationInSecs
	}
	return tDur > maxIdleDurationInSecs
}

// check if the table is start
func (t *Table) IsStart() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tStat == statInGame
}

// start the game in the table
func (t *Table) Start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.startTime = time.Now().Unix()
	t.tStat = statInGame
}

// stop the game
func (t *Table) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ready1p = false
	t.ready2p = false
	t.tStat = statWaiting
	t.startTime = time.Now().Unix()
}

// ob join the table
func (t *Table) JoinOB(u *User) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.obs.Join(u)
}

// check if the table is full
func (t *Table) IsFull() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t._1p != nil && t._2p != nil
}

// player join the Table
func (t *Table) Join(u *User) (err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case t._1p == nil:
		t._1p = u
	case t._2p == nil:
		t._2p = u
	default:
		err = ErrRoomFull
	}
	return
}

// quit a user
func (t *Table) Quit(uid int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if uid < 0 {
		return
	}
	switch uid {
	case t._1p.GetUid():
		// t._1p.Close()
		t._1p = nil
		t.ready1p = false
	case t._2p.GetUid():
		// t._2p.Close()
		t._2p = nil
		t.ready2p = false
	default:
		t.obs.Quit(uid)
	}
}

// check if the table does not have player
// for delete
func (t *Table) HasNoPlayer() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t._1p == nil && t._2p == nil
}

// get bet
func (t *Table) GetBet() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tBet
}

// get all users
func (t *Table) GetAllUsers() []int {
	t.mu.Lock()
	defer t.mu.Unlock()
	us := t.obs.GetAll()
	us = append(us, t._1p.GetUid())
	us = append(us, t._2p.GetUid())
	return us
}

// get all observers
func (t *Table) GetObservers() []int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.obs.GetAll()
}

// get 1p uid
func (t *Table) Get1pUid() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t._1p.GetUid()
}

// get 2p uid
func (t *Table) Get2pUid() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t._2p.GetUid()
}

// get user by uid
func (t *Table) GetUserById(uid int) *User {
	if uid < 0 {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch uid {
	case t._1p.GetUid():
		return t._1p
	case t._2p.GetUid():
		return t._2p
	default:
		return t.obs.GetUserById(uid)
	}
}

// close all ob connections, for game server used
func (t *Table) QuitAllObs() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.obs.QuitAll()
}

// get all players
func (t *Table) GetPlayers() []int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return []int{t._1p.GetUid(), t._2p.GetUid()}
}

// get opponent
func (t *Table) GetOpponent(uid int) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t._1p.GetUid() == uid {
		return t._2p.GetUid()
	}
	return t._1p.GetUid()
}

// check if the player is 1p or 2p
func (t *Table) Is1p(uid int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t._1p.GetUid() == uid
}

// get 1p game
func (t *Table) GetGame1p() *tetris.Game {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.g1p
}

// get 2p game
func (t *Table) GetGame2p() *tetris.Game {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.g2p
}

// check if a user is in the table
func (t *Table) IsUserExist(uid int) bool {
	if uid < 0 {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t._1p.GetUid() == uid || t._2p.GetUid() == uid || t.obs.IsUserExist(uid)
}
