//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package core

import "context"

type IncomingMessage struct {
	Channel          string
	AccountID        string
	ChatID           string
	ChatType         string
	ChatTitle        string
	MessageID        string
	ThreadID         string
	SenderID         string
	SenderName       string
	SenderUsername   string
	ReplyToMessageID string
	ReplyToSender    string
	ReplyToText      string
	ReplyToIsBot     bool
	QuoteText        string
	Text             string
	Raw              map[string]any
}

type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type OutgoingMessage struct {
	ChatID           string
	Text             string
	ReplyToMessageID string
	InlineButtons    [][]InlineButton
}

type CallbackQuery struct {
	ID        string
	Channel   string
	AccountID string
	ChatID    string
	MessageID string
	SenderID  string
	Data      string
}

type CallbackResult struct {
	ToastText     string
	InlineButtons [][]InlineButton
}

type MessageHandler func(ctx context.Context, msg IncomingMessage) error

type CallbackHandler func(ctx context.Context, cb CallbackQuery) (*CallbackResult, error)

type CallbackCapableChannel interface {
	SetCallbackHandler(CallbackHandler)
}

type Channel interface {
	ChannelID() string                                   // ChannelID return channel's name
	AccountID() string                                   // AccountID return channel's channelID
	SetMessageHandler(MessageHandler)                    // SetMessageHandler set channel's Message Handler
	Start(ctx context.Context) error                     // Start the channel
	Stop(ctx context.Context) error                      // Stop cancels channel event
	Send(ctx context.Context, msg OutgoingMessage) error // Send posts an outgoing RAGFlow answer to the channel
}
