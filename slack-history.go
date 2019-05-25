package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/bbalet/stopwords"
	"github.com/cdipaolo/goml/base"
	"github.com/cdipaolo/goml/text"
	"github.com/nlopes/slack"
)

var text2Analyze string
var outputFile = "./wordcloud.png"
var inputFile = "./text2analyze.txt"
var stopwordsFile = "./stopwords.txt"

func sanitizeNewLines(str string) string {
	noNewLine := strings.Replace(str, "\n", `\n`, -1)
	noNewLine = strings.Replace(noNewLine, "\r", `\r`, -1)
	return noNewLine
}

func writeHistory(history *slack.History, csvWriter *csv.Writer, bots bool, includeStopWords bool) error {
	//log.Printf("%#v\n", history.Messages[0:5])
	for _, m := range history.Messages {
		if !bots && m.BotID != "" {
			continue
		}

		text := m.Text
		if m.Files != nil {
			//log.Printf("%#v", m.File)
			//out, err := ioutil.ReadAll(*m.File)
			//if err != nil {
			//	log.Fatalf("Failed to read attached file. %s", err)
			//}

			for _, file := range m.Files {
				text += `\n`
				text += file.Name
				text += `\n`
				text += sanitizeNewLines(file.Preview)
			}
		}

		if len(m.Attachments) > 0 {
			for _, attachment := range m.Attachments {
				text += `\n`
				text += sanitizeNewLines(attachment.Fallback)
			}
		}

		floatTs, err := strconv.ParseFloat(m.Timestamp, 64)
		if err != nil {
			log.Printf("timestamp %s not valid, cannot parse. Err: %s", m.Timestamp, err)
		}
		//ignore milliseconds for now
		intTs := int64(floatTs)
		goTime := time.Unix(intTs, 0)

		text = strings.Replace(text, "\n", "\\n", -1)

		var cleanText string

		if !includeStopWords {
			cleanText = stopwords.CleanString(text, "en", true)
		} else {
			cleanText = text
		}

		err = csvWriter.Write([]string{goTime.Format(time.RFC3339), m.User, cleanText})
		text2Analyze += cleanText + " "

		if err != nil {
			log.Printf(`Failed to write "%s"`, cleanText)
		}
		csvWriter.Flush()

	}
	return nil
}

func textClassification() {
	// create the channel of data and errors
	stream := make(chan base.TextDatapoint, 100)
	errors := make(chan error)

	// make a new NaiveBayes model with
	// 2 classes expected (classes in
	// datapoints will now expect {0,1}.
	// in general, given n as the classes
	// variable, the model will expect
	// datapoint classes in {0,...,n-1})
	//
	// Note that the model is filtering
	// the text to omit anything except
	// words and numbers (and spaces
	// obviously)
	model := text.NewNaiveBayes(stream, 2, base.OnlyWordsAndNumbers)

	go model.OnlineLearn(errors)

	stream <- base.TextDatapoint{
		X: "I love the city",
		Y: 1,
	}

	stream <- base.TextDatapoint{
		X: "I hate Los Angeles",
		Y: 0,
	}

	stream <- base.TextDatapoint{
		X: "My mother is not a nice lady",
		Y: 0,
	}

	close(stream)

	for {
		err, _ := <-errors
		if err != nil {
			fmt.Printf("Error passed: %v", err)
		} else {
			// training is done!
			break
		}
	}

	// now you can predict like normal
	//fmt.Printf("Text to analyze: %#v\n", text2Analyze)

	// class := model.Predict(text2Analyze) // 0

	// fmt.Printf("Prediction: %v", class)
	// tfidf := text.TFIDF(*model)
	// fmt.Printf("Term frequency: %v", tfidf)

	wordCloud()
}
func createFile() {
	// detect if file exists
	var _, err = os.Stat(inputFile)

	// create file if not exists
	if !os.IsNotExist(err) {
		e := os.Remove(inputFile)
		if isError(e) {
			return
		}

		fmt.Println("==> done deleting file")
	}
	var file, e = os.Create(inputFile)
	if isError(e) {
		return
	}
	defer file.Close()
	fmt.Println("==> done creating file", inputFile)
}

func isError(err error) bool {
	if err != nil {
		fmt.Println(err.Error())
	}

	return err != nil
}

func wordCloud() {

	createFile()
	f, _ := os.OpenFile(outputFile, os.O_APPEND|os.O_RDWR, 0644)
	writer := bufio.NewWriter(f)
	fmt.Fprintln(writer, text2Analyze)
	writer.Flush()
	defer f.Close()

	cmd := exec.Command("kumo", "--input", "./output.csv", "--output", outputFile, "--word-count", "200", "--stop-words", "related,come,team,server,things,run,work,right,added,channel,wondering,bin,bunch,occured,you're,day,mean,recently,output,slightly_smiling_face,looks,pod,ok,okay,going,help,just,try,ticket,issue,service,look,thanks,need,use,like,sure,new,good,it's,want,having,don't,i'll,did,able,morning,actually,th,adding,smile,fine,default,eyes,i've,using,getting,set,check,thank,didn,problem,set,know,trying,think,looking,running,sorry,needs,info,guys,maybe,bit,that's,point,yeah,doesn't,anymore,does,tried,used,needed,weird,better,lot,correct,can't,ago,write,hi,create,following,working,file,created,hootsuite,works,i'm,stuff,type,checking,thing")
	cmd.CombinedOutput()

}

func main() {
	start := flag.String("start", "2017-06-01T00:00:00-07:00", "Start Time in ISO8601")
	end := flag.String("end", "", "End Time in ISO8601 (default is current time)")
	channel := flag.String("channel", "devops", "Channel Name to get logs for")
	filePath := flag.String("write", "output.csv", "where to output the file")
	bots := flag.Bool("bots", false, "include bot messages")
	stopwords := flag.Bool("stopwords", false, "include stop words")

	flag.Parse()
	token := os.Getenv("SLACK_TOKEN")
	if token == "" {
		log.Fatal("Need SLACK_TOKEN env variable")
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	api := slack.New(token)
	// If you set debugging, it will log all requests to the console
	// Useful when encountering issues
	// api.SetDebug(true)
	channels, err := api.GetChannels(true)
	if err != nil {
		log.Fatalf("Failed to get channels. %s", err)
	}
	channelID := "C65PEKN9J"
	for _, ch := range channels {
		if ch.Name == *channel {
			channelID = ch.ID
		}
		//fmt.Printf("Channel: %#v\n", ch.Name)
	}

	if channelID == "" {
		log.Fatalf("Cannot find a channel with name %s", *channel)
	}
	file, err := os.Create(*filePath)
	if err != nil {
		log.Fatalf("cannot create file %s", filePath)
	}

	defer file.Close()

	csvWriter := csv.NewWriter(file)
	//defer csvWriter.Close()

	columns := []string{"timestamp", "user", "message"}

	csvWriter.Write(columns)
	csvWriter.Flush()

	//log.Printf("Channels: %v", channels)
	var endTime time.Time
	historyParams := slack.NewHistoryParameters()
	startTime, err := time.Parse(time.RFC3339, *start)
	if err != nil {
		log.Fatalf("Failed to parse start timestamp %s. Err: %s", *start, err)
	}
	startTimeTs := startTime.Unix() //* 1000
	if *end != "" {
		endTime, err = time.Parse(time.RFC3339, *end)
		if err != nil {
			log.Fatalf("Failed to parse end timestamp %s. Err: %s", *end, err)
		}
	} else {
		endTime = time.Now()
	}
	endTimeTs := endTime.Unix() //* 1000
	historyParams.Latest = strconv.Itoa(int(endTimeTs))
	historyParams.Oldest = strconv.Itoa(int(startTimeTs))
	//historyParams.Count = 1000
	history, err := api.GetChannelHistory(channelID, historyParams)
	if err != nil {
		log.Fatalf("Failed to get history for channel. %s", err)
	}

	//fmt.Println("latest", endTimeTs)

	//log.Println(len(history.Messages))
	writeHistory(history, csvWriter, *bots, *stopwords)
	lastMessageTs := history.Messages[len(history.Messages)-1].Timestamp
	log.Println("lastMessageTs", lastMessageTs)
	if history.HasMore {
		for {
			log.Println("Querying more from", lastMessageTs, "startTimeTs", startTimeTs)
			historyParams.Latest = lastMessageTs
			history, err := api.GetChannelHistory(channelID, historyParams)
			if err != nil {
				log.Fatalf("Failed to get history for channel. %s", err)
			}
			writeHistory(history, csvWriter, *bots, *stopwords)
			lastMessageTs = history.Messages[len(history.Messages)-1].Timestamp
			//log.Printf("history %+v", history.Messages)
			//log.Println("lastMessageTs", lastMessageTs)
			if !history.HasMore {
				break
			}
			time.Sleep(1 * time.Second)
		}
	}

	fmt.Println("Wrote", *filePath)
	textClassification()
	//fmt.Printf("%#v\n", history)
}
