package asdu

import (
	"fmt"

	"iec104rt/internal/protocol/common"
)

var readActivateResultMap = map[int]string{0: "成功", 1: "失败"}
var writeActivateResultMap = map[int]string{0: "成功", 1: "未知错误", 2: "文件名不支持", 3: "长度超范围"}
var transferConfirmMap = map[int]string{
	0: "成功/无后续", 1: "有后续/未知错误", 2: "校验和错误",
	3: "文件长度不对应", 4: "文件ID与激活ID不一致",
}

// parseFileService210Op34 处理 op=3/4/5/6/7/8/10 及兜底。
func parseFileService210Op34(r *common.Reader, op int, result M) {
	switch op {
	case 3: // 读文件激活：fileNameLen(1)+fileName(N)
		if r.Left() < 1 {
			return
		}
		fileNameLen := r.U8()
		if r.Left() >= fileNameLen {
			result["fileNameLen"] = fileNameLen
			result["fileName"] = r.Ascii(fileNameLen)
		}
	case 4: // 读文件激活确认：resultCode(1)+fileNameLen(1)+fileName(N)+fileId(4)+fileSize(4)
		if r.Left() < 2 {
			return
		}
		resultCode := r.U8()
		fileNameLen := r.U8()
		if r.Left() < fileNameLen+8 {
			return
		}
		fileName := r.Ascii(fileNameLen)
		fileId := readU32LE(r)
		fileSize := readU32LE(r)
		rd, ok := readActivateResultMap[resultCode]
		if !ok {
			rd = fmt.Sprintf("未知(%d)", resultCode)
		}
		result["resultCode"] = resultCode
		result["resultDesc"] = rd
		result["fileNameLen"] = fileNameLen
		result["fileName"] = fileName
		result["fileId"] = fileId
		result["fileSize"] = fileSize
	case 5, 9: // 读/写文件数据传输：fileId(4,LE)+segmentNo(4,LE)+后续标志(1)+data+checksum(1)
		if r.Left() < 10 {
			return
		}
		fileId := readU32LE(r)
		segmentNo := readU32LE(r)
		contFlag := r.U8()
		dataLen := r.Left() - 1
		fileData := r.Slice(dataLen)
		checksum := r.U8()
		csCalc := common.Sum8(fileData)
		contName := "有后续"
		if contFlag == 0 {
			contName = "无后续"
		}
		result["fileId"] = fileId
		result["segmentNo"] = segmentNo
		result["contFlag"] = contFlag
		result["contFlagName"] = contName
		result["checksum"] = checksum
		result["checksumHex"] = fmt.Sprintf("0x%02X", checksum)
		result["dataLen"] = len(fileData)
		result["dataHex"] = common.HexWrap(common.HexBytes(fileData))
		result["checksumCalc"] = csCalc
		result["checksumCalcHex"] = fmt.Sprintf("0x%02X", csCalc)
		result["checksumValid"] = csCalc == checksum
	case 6, 10: // 文件数据传输确认：fileId(4,LE)+segmentNo(4,LE)+resultCode(1)
		if r.Left() < 9 {
			return
		}
		fileId := readU32LE(r)
		segmentNo := readU32LE(r)
		resultCode := r.U8()
		resultMap := map[int]string{0: "无后续", 1: "有后续"}
		if op == 10 {
			resultMap = transferConfirmMap
		}
		rd, ok := resultMap[resultCode]
		if !ok {
			rd = fmt.Sprintf("未知(%d)", resultCode)
		}
		result["fileId"] = fileId
		result["segmentNo"] = segmentNo
		result["resultCode"] = resultCode
		result["resultDesc"] = rd
	case 7: // 写文件激活：fileNameLen(1)+fileName(N)+fileId(4)+fileSize(4)
		if r.Left() < 1 {
			return
		}
		fileNameLen := r.U8()
		if r.Left() < fileNameLen+8 {
			return
		}
		result["fileNameLen"] = fileNameLen
		result["fileName"] = r.Ascii(fileNameLen)
		result["fileId"] = readU32LE(r)
		result["fileSize"] = readU32LE(r)
	case 8: // 写文件激活确认：resultCode(1)+fileNameLen(1)+fileName(N)+fileId(4)+fileSize(4)
		if r.Left() < 2 {
			return
		}
		resultCode := r.U8()
		fileNameLen := r.U8()
		if r.Left() < fileNameLen+8 {
			return
		}
		rd, ok := writeActivateResultMap[resultCode]
		if !ok {
			rd = fmt.Sprintf("未知(%d)", resultCode)
		}
		result["resultCode"] = resultCode
		result["resultDesc"] = rd
		result["fileNameLen"] = fileNameLen
		result["fileName"] = r.Ascii(fileNameLen)
		result["fileId"] = readU32LE(r)
		result["fileSize"] = readU32LE(r)
	default:
		result["rawHex"] = common.HexBytes(r.Slice(r.Left()))
	}
}

// ParsePrivateFileDir TI=210(0xD2) 私有扩展文件目录服务（南网扩展）。
// 信息体：IOA + SubType(1) + FileCount(1) + Reserved(4) + FileCount×[NameLen+Name+FileType+Start(7)+End(7)]。
func ParsePrivateFileDir(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	subTypeMap := map[int]string{0x01: "按条数查询", 0x02: "按时间范围查询", 0x03: "按序号查询"}
	fileTypeMap := map[int]string{0x00: "无时间过滤", 0x01: "按时间段过滤"}
	fileNameTypeMap := map[string]string{
		"HISTORY/COMTRADE": "录波文件", "COMTRADE": "录波文件",
		"HISTORY/SOE": "SOE事件记录", "SOE": "SOE事件记录",
		"HISTORY/CTRL": "遥控操作记录", "CTRL": "遥控操作记录",
		"HISTORY/EXTREME": "极值记录", "EXTREME": "极值记录",
		"HISTORY/FIXED": "定点记录", "FIXED": "定点记录",
		"HISTORY/LOG": "日志记录", "LOG": "日志记录",
		"HISTORY/USERLOG": "自定义日志", "USERLOG": "自定义日志",
	}
	if r.Left() < ioaLen+6 {
		return nil, fmt.Errorf("TI=0xD2 too short")
	}
	r.Skip(ioaLen)
	subType := r.U8()
	fileCount := r.U8()
	r.Skip(4) // reserved
	files := []M{}
	for i := 0; i < fileCount; i++ {
		if r.Left() < 1 {
			break
		}
		nLen := r.U8()
		if r.Left() < nLen+1+14 {
			break
		}
		name := r.Ascii(nLen)
		fileType := r.U8()
		startTime, _ := r.CP56()
		r.Skip(7)
		endTime, _ := r.CP56()
		r.Skip(7)
		ftn, ok := fileTypeMap[fileType]
		if !ok {
			ftn = fmt.Sprintf("类型0x%02X", fileType)
		}
		category, ok := fileNameTypeMap[toUpper(name)]
		if !ok {
			category = name
		}
		files = append(files, M{"name": name, "fileType": fileType, "fileTypeName": ftn,
			"fileCategory": category, "startTime": startTime, "endTime": endTime})
	}
	cause := cotRaw & 0x3F
	isRequest := cause == 5 || cause == 6
	cats := map[string]bool{}
	for _, f := range files {
		cats[f["fileCategory"].(string)] = true
	}
	allCat := ""
	for c := range cats {
		if allCat != "" {
			allCat += "、"
		}
		allCat += c
	}
	if allCat == "" {
		allCat = "文件"
	}
	desc := fmt.Sprintf("文件目录响应(%d个文件)", len(files))
	if isRequest {
		desc = fmt.Sprintf("召唤文件目录请求(%s)", allCat)
	}
	stn, ok := subTypeMap[subType]
	if !ok {
		stn = fmt.Sprintf("SubType=0x%02X", subType)
	}
	return M{"type": "file_service", "service": "PRIV_DIR", "desc": desc,
		"subType": subType, "subTypeName": stn, "fileCount": fileCount, "files": files,
		"cot": cotRaw, "addr": addr}, nil
}

// readU32LE 读取 4 字节小端无符号整数（文件服务中的目录 ID/文件 ID/大小/段号均为低字节在前）。
func readU32LE(r *common.Reader) uint32 {
	if r.Left() < 4 {
		return 0
	}
	b := r.Slice(4)
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func toUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}
