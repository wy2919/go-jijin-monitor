package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ======================= 结构体定义 ==========================

// JSONData 原始数据结构体
type JSONData struct {
	Symbol        string     `json:"symbol"`        // 代码 sz169201
	Name          string     `json:"name"`          // 名称
	Trade         *big.Float `json:"trade"`         // 最新价
	Pricechange   *big.Float `json:"pricechange"`   // 涨跌额
	Changepercent *big.Float `json:"changepercent"` // 涨跌幅
	Buy           *big.Float `json:"buy"`           // 买入
	Sell          *big.Float `json:"sell"`          // 卖出
	Settlement    *big.Float `json:"settlement"`    // 昨收
	Open          *big.Float `json:"open"`          // 今开
	High          *big.Float `json:"high"`          // 最高
	Low           *big.Float `json:"low"`           // 最低
	Volume        int        `json:"volume"`        // 成交量
	Amount        int        `json:"amount"`        // 成交额
	Code          string     `json:"code"`          // 代码 169201
	Ticktime      string     `json:"ticktime"`      // 更新时间
}

// CodeRule 监控基金结构体
type CodeRule struct {
	Code string     // 纯数字代码 169201
	Up   *big.Float // 涨初始百分比
	Down *big.Float // 跌初始百分比
}

// LogData 通知记录结构体
type LogData struct {
	InitPrice bool // 判断今天是否已经发送了高开低开通知
	UpIndex   int  // 涨通知索引
	DownIndex int  // 跌通知索引
}

var LogMap = make(map[string]*LogData)

// ======================= 工具 ==========================

func SendWx(text string) {
	param := strings.NewReader(`{"msgtype":"text","text":{"content":"` + text + `"}}`)
	req, _ := http.NewRequest("POST", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key="+*wxKey, param)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("发送到企业微信错误: %v", err)
	}
	defer resp.Body.Close()
}

// 特殊数字字符表
// var specialDigits = []rune{'𝟎', '𝟏', '𝟐', '𝟑', '𝟒', '𝟓', '𝟔', '𝟕', '𝟖', '𝟗'}
var specialDigits = []rune{'𝟬', '𝟭', '𝟮', '𝟯', '𝟰', '𝟱', '𝟲', '𝟳', '𝟴', '𝟵'}

// 替换函数：将普通数字替换为特殊数字
func r(input string) string {
	var result strings.Builder
	for _, char := range input {
		if unicode.IsDigit(char) {
			result.WriteRune(specialDigits[char-'0'])
		} else {
			result.WriteRune(char)
		}
	}
	return result.String()
}

func PrintLog(msg string) {
	log.Println(msg)
	//if *wxKey != "" {
	//	SendWx(msg)
	//}
	SendWx(msg)
}

// 生成前N位斐波那契数列 [1 2 3 5 8 13 21 34 55 89]
func generateFibonacciSequence(n int) []int {
	fib := make([]int, n)
	fib[0], fib[1] = 1, 2
	for i := 2; i < n; i++ {
		fib[i] = fib[i-1] + fib[i-2]
	}
	return fib
}

// 生成前10位斐波那契
var fibonacciSequence = generateFibonacciSequence(20)

// IsFibonacciSequence 判断斐波那契数列通知倍数 返回索引
//
//	currentChange 涨跌百分比
//	baseThreshold 默认阈值
//	currentThresholdIndex 开始索引
func IsFibonacciSequence(currentChange *big.Float, baseThreshold *big.Float, fibonacciSequence []int, currentThresholdIndex int) int {
	change, _ := currentChange.Float64()
	base, _ := baseThreshold.Float64()

	// 计算当前斐波那契阈值（首次阈值的倍数）
	currentThreshold := base * float64(fibonacciSequence[currentThresholdIndex])

	// 如果当前的价格变化超过了下一个阈值，则通知
	if math.Abs(change) >= currentThreshold {
		// 更新到下一个斐波那契倍数，返回新的索引
		return currentThresholdIndex + 1
	}
	// 未达到阈值，保持当前索引不变
	return currentThresholdIndex
}

// UnmarshalJSON 自定义反序列化器，用于处理 *big.Float 字段的 JSON 解析
func (jd *JSONData) UnmarshalJSON(data []byte) error {
	// 创建临时结构体来处理 JSON 的基本反序列化
	type Alias JSONData
	aux := &struct {
		Trade         string `json:"trade"`
		Pricechange   string `json:"pricechange"`
		Changepercent string `json:"changepercent"`
		Buy           string `json:"buy"`
		Sell          string `json:"sell"`
		Settlement    string `json:"settlement"`
		Open          string `json:"open"`
		High          string `json:"high"`
		Low           string `json:"low"`
		*Alias
	}{
		Alias: (*Alias)(jd),
	}

	// 先使用默认的 JSON 解析
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	// 解析字符串为 *big.Float 类型
	jd.Trade = stringToBigFloat(aux.Trade)
	jd.Pricechange = stringToBigFloat(aux.Pricechange)
	jd.Changepercent = stringToBigFloat(aux.Changepercent)
	jd.Buy = stringToBigFloat(aux.Buy)
	jd.Sell = stringToBigFloat(aux.Sell)
	jd.Settlement = stringToBigFloat(aux.Settlement)
	jd.Open = stringToBigFloat(aux.Open)
	jd.High = stringToBigFloat(aux.High)
	jd.Low = stringToBigFloat(aux.Low)

	return nil
}

// stringToBigFloat 将字符串解析为 *big.Float
func stringToBigFloat(s string) *big.Float {
	f := new(big.Float)
	f.SetString(s)
	return f
}

// GetLogData 获取当天的LogData
func GetLogData(code string) *LogData {
	// 获取当前时间
	time := time.Now().Format("2006-01-02")
	key := code + time
	data, ok := LogMap[key]
	if ok {
		return data
	} else {
		// 初始化新的 LogData
		newData := &LogData{
			InitPrice: false,
		}
		LogMap[key] = newData
		return newData
	}
}

// 解析参数字符串为 CodeRule 结构体切片
func parseCodes(codes string) []CodeRule {
	var rules []CodeRule
	items := strings.Split(codes, ",")
	for _, item := range items {
		parts := strings.Split(item, "-")
		if len(parts) == 3 {
			up, _ := new(big.Float).SetString(parts[1])
			down, _ := new(big.Float).SetString(parts[2])
			r := CodeRule{
				Code: parts[0],
				Up:   up,
				Down: down,
			}
			rules = append(rules, r)
		}
	}
	return rules
}

// 将 JSONData 切片转换为 map
func convertToMap(data []JSONData) map[string]JSONData {
	resultMap := make(map[string]JSONData)
	for _, item := range data {
		resultMap[item.Code] = item
	}
	return resultMap
}

// calculatePercentageChange 计算价格1和价格2的价差百分比
func calculatePercentageChange(price1, price2 *big.Float) *big.Float {
	// (price1 - price2)
	diff := new(big.Float).Sub(price1, price2)

	// (diff / price1)
	percentageChange := new(big.Float).Quo(diff, price1)

	// 乘以 100 转换为百分比
	percentageChange.Mul(percentageChange, big.NewFloat(100))

	return percentageChange
}

// 从新浪网站获取基金数据
func fetchFundData(symbol string) ([]JSONData, error) {
	// 基金类型映射
	fundMap := map[string]string{
		"封闭式基金": "close_fund",
		"ETF基金": "etf_hq_fund",
		"LOF基金": "lof_hq_fund",
	}

	// 构造请求URL和参数
	url := "http://vip.stock.finance.sina.com.cn/quotes_service/api/jsonp.php/IO.XSRV2.CallbackList['da_yPT46_Ll7K6WD']/Market_Center.getHQNodeDataSimple"
	params := "?page=1&num=1000&sort=symbol&asc=0&node=" + fundMap[symbol]

	// 发起HTTP请求
	resp, err := http.Get(url + params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 读取响应内容
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析响应中的 JSONP 数据
	dataText := string(body)

	jsonStart := strings.Index(dataText, "([") + 1
	jsonEnd := strings.LastIndex(dataText, "])")
	jsonData := dataText[jsonStart : jsonEnd+1]

	// 解析 JSON 数据
	var data []JSONData

	// 解析 JSON
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		fmt.Println("JSON解析错误:", err)
		return nil, err
	}

	return data, nil
}

// IsInitPrice 每天第一次计算昨收和今开的差，跌就通知，每天只通知一次
func IsInitPrice(dataItem JSONData, logStr *string) {

	// 计算涨跌百分比
	ratio := calculatePercentageChange(dataItem.Open, dataItem.Settlement)

	s := ""

	switch dataItem.Settlement.Cmp(dataItem.Open) {
	case -1:
		// 昨收 小于 今开
		s = "🔴高开"
	case 0:
		// 昨收 等于 今开
	case 1:
		// 昨收 大于 今开
		s = "🟢低开"
	}

	// 判断今天有没有通知过
	if !GetLogData(dataItem.Code).InitPrice {
		GetLogData(dataItem.Code).InitPrice = true
		*logStr += fmt.Sprintf("【%s】%s %s%%\n\n", dataItem.Name, s, r(ratio.Text('f', 2)))
	}
}

// IsUpDownPrice 判断今开和当前价格差
func IsUpDownPrice(codeItem CodeRule, dataItem JSONData, logStr *string) {

	// 计算涨跌百分比
	ratio := calculatePercentageChange(dataItem.Trade, dataItem.Open)

	switch dataItem.Open.Cmp(dataItem.Trade) {
	case -1:
		// 今开 小于 当前 (涨)

		// 获取斐波那契数列最大倍数
		i := GetLogData(dataItem.Code).UpIndex
		for {
			// 计算返回新下标
			index := IsFibonacciSequence(ratio, codeItem.Up, fibonacciSequence, GetLogData(dataItem.Code).UpIndex)
			if index != GetLogData(dataItem.Code).UpIndex {
				GetLogData(dataItem.Code).UpIndex = index
			} else {
				break
			}
		}

		if GetLogData(dataItem.Code).UpIndex != i {
			//*logStr += fmt.Sprintf("🔴【%s】%d X %s%% = %s%%\n\n", dataItem.Name, fibonacciSequence[GetLogData(dataItem.Code).UpIndex-1], codeItem.Up.Text('f', 2), ratio.Text('f', 2))
			*logStr += fmt.Sprintf("【%s】🔴日内 %s%%\n\n", dataItem.Name, r(ratio.Text('f', 2)))
		}
	case 0:
		// 今开 等于 当前
	case 1:
		// 今开 大于 当前 (跌)

		// 获取斐波那契数列最大倍数
		i := GetLogData(dataItem.Code).DownIndex
		for {
			// 计算返回新下标
			index := IsFibonacciSequence(ratio, codeItem.Down, fibonacciSequence, GetLogData(dataItem.Code).DownIndex)
			if index != GetLogData(dataItem.Code).DownIndex {
				GetLogData(dataItem.Code).DownIndex = index
			} else {
				break
			}
		}

		if GetLogData(dataItem.Code).DownIndex != i {
			*logStr += fmt.Sprintf("【%s】🟢日内 %s%%\n\n", dataItem.Name, r(ratio.Text('f', 2)))
		}
	}
}

func Task(logStr *string, wg *sync.WaitGroup) {
	defer wg.Done()

	// 1.代码  2.涨百分比  3.跌百分比
	//codes := "159973-0.10-0.01,511130-0.10-0.01"
	codeArr := parseCodes(*codes)
	data1, err := fetchFundData("ETF基金")
	if err != nil {
		*logStr += fmt.Sprintf("从【ETF基金】Api获取数据时出错：%v\n\n", err)
		return
	}

	data2, err := fetchFundData("LOF基金")
	if err != nil {
		*logStr += fmt.Sprintf("从【LOF基金】Api获取数据时出错：%v\n\n", err)
		return
	}

	// 合并切片
	data := append(data1, data2...)

	// 将切片转换为 map
	dataMap := convertToMap(data)

	for _, codeItem := range codeArr {
		item, ok := dataMap[codeItem.Code]
		if ok {
			IsInitPrice(item, logStr)
			IsUpDownPrice(codeItem, item, logStr)
		} else {
			*logStr += fmt.Sprintf("code参数错误！没有找到该【%s】对应的基金\n\n", codeItem.Code)
		}
	}
}

var codes = flag.String("codes", "", "代码规则")
var wxKey = flag.String("wxKey", "", "企业微信WebHook的key")
var second = flag.Int64("interval", 30, "监听间隔 单位：秒 默认30")

func main() {

	flag.Parse()

	ticker := time.NewTicker(time.Duration(*second) * time.Second)

	go func() {
		for {
			select {
			case <-ticker.C:
				logStr := ""

				// 创建计数器
				var wg sync.WaitGroup
				wg.Add(1)

				go Task(&logStr, &wg)

				wg.Wait()

				if logStr != "" {
					PrintLog(strings.TrimSuffix(logStr, "\n\n"))
				}
			}
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	for {
		select {
		case <-c:
			return
		}
	}
}
