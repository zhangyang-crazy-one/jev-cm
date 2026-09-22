package tokens

const EstimatorName = "utf8-div-4"
const Limit = 32_000

func Estimate(text string) int {
	if text == "" {
		return 0
	}
	n := (len([]byte(text)) + 3) / 4
	if n < 1 {
		return 1
	}
	return n
}
