package accounts

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"starlight/gameapi/pkg/model"
)

// Explicit projection: do not embed User or AgentNode here.
type AgentExportNode struct {
	ID              string `json:"id"`
	Username        string `json:"username"`
	InviterID       string `json:"inviter_id"`
	InviterUsername string `json:"inviter_username"`
	Status          string `json:"status"`
	MissingParent   bool   `json:"missing_parent"`
	Deposit         int64  `json:"deposit_cents"`
	DirectCount     int    `json:"direct_count"`
	TeamCount       int    `json:"team_count"`
	TeamDeposit     int64  `json:"team_deposit_cents"`
	TodayStake      int64  `json:"today_stake_cents"`
	TeamTodayStake  int64  `json:"team_today_stake_cents"`
}

type AgentExport struct {
	Items      []AgentExportNode `json:"items"`
	Day        string            `json:"day"`
	TodayStake int64             `json:"today_stake_cents"`
	ExportedAt string            `json:"exported_at"`
	Scope      string            `json:"scope"`
}

func (s *Store) agentExportAt(actor, username string, now time.Time) (AgentExport, error) {
	p, err := s.agentTreeSnapshot(actor, username, "", 1, now, true)
	out := AgentExport{Items: []AgentExportNode{}, Day: p.Day, TodayStake: p.TodayStake, ExportedAt: now.In(model.Beijing).Format(time.RFC3339), Scope: strings.TrimSpace(username)}
	if err != nil {
		return out, err
	}
	for _, n := range p.Items {
		out.Items = append(out.Items, AgentExportNode{n.ID, n.Username, n.InviterID, n.InviterUsername, n.Status, n.MissingParent, n.Deposit, n.DirectCount, n.TeamCount, n.TeamDeposit, n.TodayStake, n.TeamTodayStake})
	}
	return out, nil
}

func (h *HTTP) exportAgents(w http.ResponseWriter, r *http.Request, u User) {
	q := r.URL.Query()
	format := q.Get("format")
	for _, key := range []string{"format", "username", "parent_id", "page"} {
		if len(q[key]) > 1 {
			result(w, nil, errors.New("导出参数不可重复"))
			return
		}
	}
	if (format != "xlsx" && format != "data") || q.Get("parent_id") != "" || q.Get("page") != "" {
		result(w, nil, errors.New("导出仅支持 format=xlsx|data，不接受 parent_id/page"))
		return
	}
	v, err := h.Store.agentExportAt(u.ID, q.Get("username"), time.Now())
	if err != nil {
		result(w, nil, err)
		return
	}
	if format == "data" {
		result(w, v, nil)
		return
	}
	raw, err := agentXLSX(v)
	if err != nil {
		result(w, nil, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	exportedAt, _ := time.Parse(time.RFC3339, v.ExportedAt)
	w.Header().Set("Content-Disposition", `attachment; filename="agent-relations-`+exportedAt.Format("20060102-150405")+`.xlsx"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

const agentExcelMaxRows = 1048576 // Includes the header; never truncate.

func agentXLSX(v AgentExport) ([]byte, error) {
	if len(v.Items) > agentExcelMaxRows-1 {
		return nil, errors.New("导出超过Excel上限1048575个账号，请缩小账号范围")
	}
	if v.TodayStake < 0 || v.TodayStake > 999999999999999 {
		return nil, errors.New("今日总流水超出Excel精确数值范围")
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	files := []struct{ name, body string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="代理关系" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"xl/styles.xml", `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="1"><font><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="3"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="49" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/><xf numFmtId="2" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs></styleSheet>`},
	}
	for _, f := range files {
		// Add the metadata sheet to the package relationships.
		switch f.name {
		case "[Content_Types].xml":
			f.body = strings.Replace(f.body, "</Types>", `<Override PartName="/xl/worksheets/sheet2.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`, 1)
		case "xl/workbook.xml":
			f.body = strings.Replace(f.body, "</sheets>", `<sheet name="导出说明" sheetId="2" r:id="rId3"/></sheets>`, 1)
		case "xl/_rels/workbook.xml.rels":
			f.body = strings.Replace(f.body, "</Relationships>", `<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/></Relationships>`, 1)
		case "xl/styles.xml":
			f.body = strings.Replace(f.body, `fonts count="1"`, `fonts count="2"`, 1)
			f.body = strings.Replace(f.body, "</fonts>", `<font><b/><sz val="11"/><name val="Calibri"/></font></fonts>`, 1)
			f.body = strings.Replace(f.body, `fills count="2"`, `fills count="3"`, 1)
			f.body = strings.Replace(f.body, "</fills>", `<fill><patternFill patternType="solid"><fgColor rgb="FFE8EEF7"/><bgColor indexed="64"/></patternFill></fill></fills>`, 1)
			f.body = strings.Replace(f.body, `cellXfs count="3"`, `cellXfs count="4"`, 1)
			f.body = strings.Replace(f.body, "</cellXfs>", `<xf numFmtId="49" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1" applyNumberFormat="1"/></cellXfs>`, 1)
			f.body = strings.Replace(f.body, "</styleSheet>", `<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`, 1)
		}
		w, err := z.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte(xml.Header + f.body)); err != nil {
			return nil, err
		}
	}
	w, err := z.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	var sheet bytes.Buffer
	fmt.Fprint(&sheet, xml.Header+`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols><col min="1" max="2" width="26" customWidth="1"/><col min="3" max="3" width="14" customWidth="1"/><col min="4" max="5" width="18" customWidth="1"/><col min="6" max="9" width="26" customWidth="1"/></cols><sheetData>`)
	writeRow := func(row int, texts []string, nums []int64) {
		fmt.Fprintf(&sheet, `<row r="%d">`, row)
		for i, text := range texts {
			style := 1
			if row == 1 {
				style = 3
			}
			fmt.Fprintf(&sheet, `<c r="%c%d" t="inlineStr" s="%d"><is><t xml:space="preserve">`, 'A'+i, row, style)
			_ = xml.EscapeText(&sheet, []byte(text))
			fmt.Fprint(&sheet, `</t></is></c>`)
		}
		for i, n := range nums {
			style, value := 0, strconv.FormatInt(n, 10)
			if i >= 2 {
				// Excel stores 15 significant digits; fail instead of losing cents.
				style = 2
				value = fmt.Sprintf("%d.%02d", n/100, n%100)
			}
			fmt.Fprintf(&sheet, `<c r="%c%d" t="n" s="%d"><v>%s</v></c>`, 'A'+len(texts)+i, row, style, value)
		}
		fmt.Fprint(&sheet, `</row>`)
	}
	writeRow(1, []string{"用户名", "上级", "状态", "直属下级(人)", "全部下级(人)", "个人累计充值(元)", "下级累计充值(元)", "个人今日流水(元)", "下级今日流水(元)"}, nil)
	for i, n := range v.Items {
		for _, money := range []int64{n.Deposit, n.TeamDeposit, n.TodayStake, n.TeamTodayStake} {
			if money < 0 || money > 999999999999999 {
				return nil, errors.New("金额超出Excel精确数值范围")
			}
		}
		parent := n.InviterUsername
		if n.MissingParent {
			parent = "记录缺失"
		} else if parent == "" {
			parent = "无上级"
		}
		status := "正常"
		if n.Status == "deleted" {
			status = "已删除"
		} else if n.Status != "active" {
			status = "已停用"
		}
		writeRow(i+2, []string{n.Username, parent, status}, []int64{int64(n.DirectCount), int64(n.TeamCount), n.Deposit, n.TeamDeposit, n.TodayStake, n.TeamTodayStake})
	}
	fmt.Fprintf(&sheet, `</sheetData><autoFilter ref="A1:I%d"/></worksheet>`, len(v.Items)+1)
	if _, err = w.Write(sheet.Bytes()); err != nil {
		return nil, err
	}
	w, err = z.Create("xl/worksheets/sheet2.xml")
	if err != nil {
		return nil, err
	}
	var metadata bytes.Buffer
	fmt.Fprint(&metadata, xml.Header+`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><cols><col min="1" max="1" width="30" customWidth="1"/><col min="2" max="2" width="100" customWidth="1"/></cols><sheetData>`)
	scope := v.Scope
	if scope == "" {
		scope = "全部账号"
	}
	for i, pair := range [][2]string{{"统计日期（北京时间）", v.Day}, {"导出时间（北京时间）", v.ExportedAt}, {"导出范围（scope）", scope}, {"今日总流水(元，含本人)", fmt.Sprintf("%d.%02d", v.TodayStake/100, v.TodayStake%100)}, {"统计口径", "下级人数、下级充值及下级流水均不含本人。今日总流水为导出范围内每个账号个人今日流水之和；不可逐行累加下级流水或团队含本人金额，以免重复计算。"}} {
		fmt.Fprintf(&metadata, `<row r="%d">`, i+1)
		for j, value := range pair {
			if i == 3 && j == 1 {
				fmt.Fprintf(&metadata, `<c r="B4" t="n" s="2"><v>%s</v></c>`, value)
				continue
			}
			fmt.Fprintf(&metadata, `<c r="%c%d" t="inlineStr" s="%d"><is><t>`, 'A'+j, i+1, 3-j*2)
			_ = xml.EscapeText(&metadata, []byte(value))
			fmt.Fprint(&metadata, `</t></is></c>`)
		}
		fmt.Fprint(&metadata, `</row>`)
	}
	fmt.Fprint(&metadata, `</sheetData></worksheet>`)
	if _, err = w.Write(metadata.Bytes()); err != nil {
		return nil, err
	}
	if err = z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
