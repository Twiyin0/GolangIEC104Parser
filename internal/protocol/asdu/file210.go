package asdu

import (
	"fmt"

	"iec104rt/internal/protocol/common"
)

// ParseFileService210 TI=210(0xD2) 私有扩展文件服务（南网/南瑞等厂家扩展）。
// 注意：参考实现中 104 与 101 的 TI=210 均按 IOA=3 字节解析（D-05）。
func ParseFileService210(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	packetTypeMap := map[int]string{1: "备用", 2: "文件传输", 3: "备用", 4: "备用"}
	opMap := map[int]string{
		1: "文件目录召唤", 2: "目录召唤确认", 3: "读文件激活", 4: "读文件激活确认",
		5: "读文件数据传输", 6: "读文件数据传输确认", 7: "写文件激活", 8: "写文件激活确认",
		9: "写文件数据传输", 10: "写文件数据传输确认",
	}
	dirResultMap := map[int]string{0: "成功", 1: "失败"}

	if r.Left() < ioaLen+1 {
		return nil, fmt.Errorf("TI=210 file service too short")
	}
	infoObjAddr := readIOA(r, ioaLen)
	packetType := r.U8()
	packetTypeName, ok := packetTypeMap[packetType]
	if !ok {
		packetTypeName = fmt.Sprintf("未知(%d)", packetType)
	}
	base := M{"type": "file_service", "service": "F_FR_NA_1", "tiName": "F_FR_NA_1",
		"infoObjAddr": infoObjAddr, "packetType": packetType, "packetTypeName": packetTypeName,
		"cot": cotRaw, "addr": addr}

	if packetType != 2 {
		base["desc"] = fmt.Sprintf("文件服务附加包类型%s", packetTypeName)
		base["rawHex"] = common.HexBytes(r.Slice(r.Left()))
		return base, nil
	}
	if r.Left() < 1 {
		return nil, fmt.Errorf("TI=210 missing operation code")
	}
	op := r.U8()
	opName, ok := opMap[op]
	if !ok {
		opName = fmt.Sprintf("未知操作(%d)", op)
	}
	result := M{}
	for k, v := range base {
		result[k] = v
	}
	result["op"] = op
	result["opName"] = opName
	result["desc"] = opName

	switch op {
	case 1: // 目录召唤：directoryId(4)+dirNameLen(1)+dirName(N)+queryFlag(1)+start(7)+end(7)
		if r.Left() < 5 {
			return nil, fmt.Errorf("目录召唤报文长度不足")
		}
		directoryId := readU32LE(r)
		dirNameLen := r.U8()
		if r.Left() < dirNameLen+1+7+7 {
			return nil, fmt.Errorf("目录召唤目录名/时间长度不足")
		}
		directoryName := r.Ascii(dirNameLen)
		queryFlag := r.U8()
		queryFlagName := fmt.Sprintf("未知(%d)", queryFlag)
		switch queryFlag {
		case 0:
			queryFlagName = "目录下所有文件"
		case 1:
			queryFlagName = "目录下满足搜索时间段的文件"
		}
		startTime, _ := r.CP56()
		r.Skip(7)
		endTime, _ := r.CP56()
		r.Skip(7)
		result["directoryId"] = directoryId
		result["directoryName"] = directoryName
		result["dirNameLen"] = dirNameLen
		result["queryFlag"] = queryFlag
		result["queryFlagName"] = queryFlagName
		result["startTime"] = startTime
		result["endTime"] = endTime
	case 2: // 目录召唤确认：resultCode(1)+directoryId(4)+hasMore(1)+fileCount(1)+entries
		if r.Left() < 7 {
			return nil, fmt.Errorf("目录召唤确认长度不足")
		}
		resultCode := r.U8()
		directoryId := readU32LE(r)
		hasMore := r.U8()
		fileCount := r.U8()
		entries := []M{}
		for i := 0; i < fileCount; i++ {
			if r.Left() < 1 {
				return nil, fmt.Errorf("目录文件名长度缺失")
			}
			nameLen := r.U8()
			if r.Left() < nameLen+1+4+7 {
				return nil, fmt.Errorf("目录文件项长度不足")
			}
			fileName := r.Ascii(nameLen)
			fileAttr := r.U8()
			fileSize := readU32LE(r)
			fileTime, _ := r.CP56()
			r.Skip(7)
			entries = append(entries, M{"fileName": fileName, "fileAttr": fileAttr, "fileSize": fileSize, "fileTime": fileTime})
		}
		rd, ok2 := dirResultMap[resultCode]
		if !ok2 {
			rd = fmt.Sprintf("未知(%d)", resultCode)
		}
		result["resultCode"] = resultCode
		result["resultDesc"] = rd
		result["directoryId"] = directoryId
		result["hasMore"] = hasMore
		result["hasMoreDesc"] = map[bool]string{true: "有后续", false: "无后续"}[hasMore != 0]
		result["fileCount"] = fileCount
		result["entries"] = entries
	default:
		parseFileService210Op34(r, op, result)
	}
	return result, nil
}
