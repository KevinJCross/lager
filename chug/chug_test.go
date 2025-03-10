package chug_test

import (
	"code.cloudfoundry.org/lager/chug"
	"errors"
	"io"
	"time"

	"code.cloudfoundry.org/lager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Chug", func() {
	var (
		logger     lager.Logger
		stream     chan chug.Entry
		pipeReader *io.PipeReader
		pipeWriter *io.PipeWriter
	)

	BeforeEach(func() {
		pipeReader, pipeWriter = io.Pipe()
		logger = lager.NewLogger("chug-test")
		logger.RegisterSink(lager.NewWriterSink(pipeWriter, lager.DEBUG))
		stream = make(chan chug.Entry, 100)
	})

	JustBeforeEach(func() {
		go chug.Chug(pipeReader, stream)
	})

	AfterEach(func() {
		pipeWriter.Close()
		Eventually(stream).Should(BeClosed())
	})

	Context("when fed a stream of well-formed lager messages", func() {
		It("should return parsed lager messages", func() {
			data := lager.Data{"some-float": 3.0, "some-string": "foo"}
			logger.Debug("chug", data)
			logger.Info("again", data)

			entry := <-stream
			Expect(entry.IsLager).To(BeTrue())
			Expect(entry.Log).To(MatchLogEntry(chug.LogEntry{
				LogLevel: lager.DEBUG,
				Source:   "chug-test",
				Message:  "chug-test.chug",
				Data:     data,
			}))

			entry = <-stream
			Expect(entry.IsLager).To(BeTrue())
			Expect(entry.Log).To(MatchLogEntry(chug.LogEntry{
				LogLevel: lager.INFO,
				Source:   "chug-test",
				Message:  "chug-test.again",
				Data:     data,
			}))

		})

		It("should parse the timestamp", func() {
			logger.Debug("chug")
			entry := <-stream
			Expect(entry.Log.Timestamp).To(BeTemporally("~", time.Now(), time.Second))
		})

		Context("when parsing an error message", func() {
			It("should include the error", func() {
				data := lager.Data{"some-float": 3.0, "some-string": "foo"}
				logger.Error("chug", errors.New("some-error"), data)
				Expect((<-stream).Log).To(MatchLogEntry(chug.LogEntry{
					LogLevel: lager.ERROR,
					Source:   "chug-test",
					Message:  "chug-test.chug",
					Error:    errors.New("some-error"),
					Data:     lager.Data{"some-float": 3.0, "some-string": "foo"},
				}))

			})
		})

		Context("when parsing an info message with an error", func() {
			It("should not take the error out of the data map", func() {
				data := lager.Data{"some-float": 3.0, "some-string": "foo", "error": "some-error"}
				logger.Info("chug", data)
				Expect((<-stream).Log).To(MatchLogEntry(chug.LogEntry{
					LogLevel: lager.INFO,
					Source:   "chug-test",
					Message:  "chug-test.chug",
					Error:    nil,
					Data:     lager.Data{"some-float": 3.0, "some-string": "foo", "error": "some-error"},
				}))

			})
		})

		Context("when multiple sessions have been established", func() {
			It("should build up the task array appropriately", func() {
				firstSession := logger.Session("first-session")
				firstSession.Info("encabulate")
				nestedSession := firstSession.Session("nested-session-1")
				nestedSession.Info("baconize")
				firstSession.Info("remodulate")
				nestedSession.Info("ergonomize")
				nestedSession = firstSession.Session("nested-session-2")
				nestedSession.Info("modernify")

				Expect((<-stream).Log).To(MatchLogEntry(chug.LogEntry{
					LogLevel: lager.INFO,
					Source:   "chug-test",
					Message:  "chug-test.first-session.encabulate",
					Session:  "1",
					Data:     lager.Data{},
				}))

				Expect((<-stream).Log).To(MatchLogEntry(chug.LogEntry{
					LogLevel: lager.INFO,
					Source:   "chug-test",
					Message:  "chug-test.first-session.nested-session-1.baconize",
					Session:  "1.1",
					Data:     lager.Data{},
				}))

				Expect((<-stream).Log).To(MatchLogEntry(chug.LogEntry{
					LogLevel: lager.INFO,
					Source:   "chug-test",
					Message:  "chug-test.first-session.remodulate",
					Session:  "1",
					Data:     lager.Data{},
				}))

				Expect((<-stream).Log).To(MatchLogEntry(chug.LogEntry{
					LogLevel: lager.INFO,
					Source:   "chug-test",
					Message:  "chug-test.first-session.nested-session-1.ergonomize",
					Session:  "1.1",
					Data:     lager.Data{},
				}))

				Expect((<-stream).Log).To(MatchLogEntry(chug.LogEntry{
					LogLevel: lager.INFO,
					Source:   "chug-test",
					Message:  "chug-test.first-session.nested-session-2.modernify",
					Session:  "1.2",
					Data:     lager.Data{},
				}))

			})
		})

		Context("when the input is formatted with human readable timestamps", func() {
			BeforeEach(func() {
				logger = lager.NewLogger("chug-test")
				logger.RegisterSink(lager.NewPrettySink(pipeWriter, lager.DEBUG))
			})

			It("should return parsed lager messages", func() {
				data := lager.Data{"some-float": 3.0, "some-string": "foo"}
				logger.Debug("chug", data)
				logger.Info("again", data)

				entry := <-stream
				Expect(entry.IsLager).To(BeTrue())
				Expect(entry.Log).To(MatchLogEntry(chug.LogEntry{
					LogLevel: lager.DEBUG,
					Source:   "chug-test",
					Message:  "chug-test.chug",
					Data:     data,
				}))

				entry = <-stream
				Expect(entry.IsLager).To(BeTrue())
				Expect(entry.Log).To(MatchLogEntry(chug.LogEntry{
					LogLevel: lager.INFO,
					Source:   "chug-test",
					Message:  "chug-test.again",
					Data:     data,
				}))
			})
		})
	})

	Context("handling lager JSON that is surrounded by non-JSON", func() {
		var input []byte
		var entry chug.Entry

		JustBeforeEach(func() {
			input = []byte(`[some-component][e]{"timestamp":"1407102779.028711081","source":"chug-test","message":"chug-test.chug","log_level":0,"data":{"some-float":3,"some-string":"foo"}}...some trailing stuff`)
			pipeWriter.Write(input)
			pipeWriter.Write([]byte("\n"))

			Eventually(stream).Should(Receive(&entry))
		})

		It("should be a lager message", func() {
			Expect(entry.IsLager).To(BeTrue())
		})

		It("should contain all the data in Raw", func() {
			Expect(entry.Raw).To(Equal(input))
		})

		It("should succesfully parse the lager message", func() {
			Expect(entry.Log.Source).To(Equal("chug-test"))
		})
	})

	Context("handling malformed/non-lager data", func() {
		var input []byte
		var entry chug.Entry

		JustBeforeEach(func() {
			pipeWriter.Write(input)
			pipeWriter.Write([]byte("\n"))

			Eventually(stream).Should(Receive(&entry))
		})

		Context("when fed a stream of malformed lager messages", func() {
			Context("when the timestamp is invalid", func() {
				BeforeEach(func() {
					input = []byte(`{"timestamp":"tomorrow","source":"chug-test","message":"chug-test.chug","log_level":3,"data":{"some-float":3,"some-string":"foo","error":7}}`)
				})

				itReturnsRawData(entry, input)
			})

			Context("when the error does not parse", func() {
				BeforeEach(func() {
					input = []byte(`{"timestamp":"1407102779.028711081","source":"chug-test","message":"chug-test.chug","log_level":3,"data":{"some-float":3,"some-string":"foo","error":7}}`)
				})

				itReturnsRawData(entry, input)
			})

			Context("when the trace does not parse", func() {
				BeforeEach(func() {
					input = []byte(`{"timestamp":"1407102779.028711081","source":"chug-test","message":"chug-test.chug","log_level":3,"data":{"some-float":3,"some-string":"foo","trace":7}}`)
				})

				itReturnsRawData(entry, input)
			})

			Context("when the session does not parse", func() {
				BeforeEach(func() {
					input = []byte(`{"timestamp":"1407102779.028711081","source":"chug-test","message":"chug-test.chug","log_level":3,"data":{"some-float":3,"some-string":"foo","session":7}}`)
				})

				itReturnsRawData(entry, input)
			})
		})

		Context("When fed JSON that is not a lager message at all", func() {
			BeforeEach(func() {
				input = []byte(`{"source":"chattanooga"}`)
			})

			itReturnsRawData(entry, input)
		})

		Context("When fed none-JSON that is not a lager message at all", func() {
			BeforeEach(func() {
				input = []byte(`ß`)
			})

			itReturnsRawData(entry, input)
		})
	})

	Context("when writing is complete", func() {
		var input []byte
		var entry chug.Entry

		BeforeEach(func() {
			input = []byte("hello")
		})

		JustBeforeEach(func() {
			pipeWriter.Write(input)
			pipeWriter.Write([]byte("\n"))
			Expect(pipeWriter.Close()).To(Succeed())

			Eventually(stream).Should(Receive(&entry))
		})

		itReturnsRawData(entry, input)

		It("returns no more messages", func() {
			Consistently(stream).ShouldNot(Receive())
		})
	})
})

func itReturnsRawData(entry chug.Entry, input []byte) {
	It("returns raw data", func() {
		Expect(entry.IsLager).To(BeFalse())
		Expect(entry.Log).To(BeZero())
		Expect(entry.Raw).To(Equal(input))
	})
}
