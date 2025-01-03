package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
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
	Code  string     // 纯数字代码 169201
	Price *big.Float // 标定价格 上一次定投的价格
	Ratio *big.Float // 下跌%多少
}

// LogData 通知记录结构体
type LogPrice struct {
	Code  string // 代码
	Index int    // 通知索引
}

var LogMap = make(map[string]*LogPrice)

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
	if *wxKey != "" {
		SendWx(msg)
	}
	//SendWx(msg)
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
func GetLogData(code string) *LogPrice {
	// 获取当前时间
	data, ok := LogMap[code]
	if ok {
		return data
	} else {
		// 初始化新的 LogData
		newData := &LogPrice{
			Code:  code,
			Index: -1,
		}
		LogMap[code] = newData
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
			price, _ := new(big.Float).SetString(parts[1])
			ratio, _ := new(big.Float).SetString(parts[2])
			r := CodeRule{
				Code:  parts[0],
				Price: price,
				Ratio: ratio,
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

// 给定一个初始值，获取每下跌%多少的价格列表
func getPriceList(initialPrice *big.Float, decreasePercentage *big.Float) []*big.Float {

	// 定义常量 100 和 1
	oneHundred := new(big.Float).SetFloat64(100.0)
	one := new(big.Float).SetFloat64(1.0)

	// 计算下跌因子：1 - (decreasePercentage / 100)
	factor := new(big.Float).Quo(decreasePercentage, oneHundred) // decreasePercentage / 100
	multiplier := new(big.Float).Sub(one, factor)                // 1 - (decreasePercentage / 100)

	var priceList []*big.Float

	// 当前价格
	currentPrice := new(big.Float).Copy(initialPrice)

	// 计算并存储每下跌后的价格
	for i := 0; i < 100; i++ {
		// 计算下跌后的价格
		currentPrice.Mul(currentPrice, multiplier)

		// 将当前价格添加到列表中
		priceList = append(priceList, new(big.Float).Copy(currentPrice))
	}

	return priceList
}

// 判断当前价格下跌到了切片中价格的第几次
func getDecreaseStep(priceList []*big.Float, currentPrice *big.Float) int {
	// 大于第一次的下跌价格直接跳过 连初始标定价格的第一个4%都没跌到就不用判断后面的了
	if currentPrice.Cmp(priceList[0]) > 0 {
		return -1
	}

	for i, price := range priceList {
		if currentPrice.Cmp(price) <= 0 {
			continue
		} else {
			return i
		}
	}
	return -1
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

// countPrice
func countPrice(dataItem JSONData, codeRule CodeRule, logStr *string) {

	// 获取价格List
	priceList := getPriceList(codeRule.Price, codeRule.Ratio)

	// 记录前索引
	is := false
	
	// 计算返回新下标
	index := getDecreaseStep(priceList, dataItem.Trade)
	if index != -1 && index > GetLogData(dataItem.Code).Index {
		GetLogData(dataItem.Code).Index = index
		is = true
	}

	// 索引发生了变化
	if is {
		*logStr += fmt.Sprintf("【%s】：第%d次下跌到位 目标%s 当前%s \n", dataItem.Code, GetLogData(dataItem.Code).Index, r(priceList[GetLogData(dataItem.Code).Index-1].Text('f', 3)), r(dataItem.Trade.String()))
	}
}

func Task(logStr *string, wg *sync.WaitGroup) {
	defer wg.Done()
	
	// 1.代码  2.涨百分比  3.跌百分比
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

	if len(data) == 0 {
		log.Println("从新浪获取数据为0")
		return
	}

	// 将切片转换为 map
	dataMap := convertToMap(data)

	for _, codeItem := range codeArr {
		item, ok := dataMap[codeItem.Code]
		if ok {
			// 判断今开和当前价格都不为0
			if item.Open.Cmp(big.NewFloat(0.0)) != 0 && item.Trade.Cmp(big.NewFloat(0.0)) != 0 {
				countPrice(item, codeItem, logStr)
			}
		} else {
			*logStr += fmt.Sprintf("code参数错误！没有找到该【%s】对应的基金\n\n", codeItem.Code)
		}
	}
}

// var codes = flag.String("codes", "159915-2.1170-4.0,513090-1.70-4.0", "代码规则")
var codes = flag.String("codes", "", "代码规则")
var wxKey = flag.String("wxkey", "", "企业微信WebHook的key")
var second = flag.Int64("second", 60, "监听间隔 单位：秒 默认50")

func main() {

	// 定投监控

	flag.Parse()

	fmt.Printf("参数codes：%s \n", *codes)
	fmt.Printf("参数wxKey： %s \n", *wxKey)
	fmt.Printf("参数second： %d \n", *second)

	ticker := time.NewTicker(time.Duration(*second) * time.Second)

	go func() {
		for {
			select {
			case <-ticker.C:
				// 在6:00到20:00之间执行
				if time.Now().Hour() >= 6 && time.Now().Hour() < 20 {

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
