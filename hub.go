// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/pkg/errors"
)

const serverName = "simple-go-chat-demo.herokuapp.com"

// Channel is to hold its members and topic.
type Channel struct {
	Members map[*Client]struct{}
	Topic   string
}

func (c *Channel) ListMembers() []string {
	var members []string
	for c := range c.Members {
		members = append(members, c.prefix)
	}
	return members
}

func (c *Channel) IsMember(prefix string) bool {
	for c := range c.Members {
		if c.prefix == prefix {
			return true
		}
	}
	return false
}

// hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[*Client]struct{}

	// Inbound messages from the clients.
	broadcast chan *Message

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	// channels in the hub
	channels map[string]*Channel
}

func newHub() *Hub {
	return &Hub{
		broadcast:  make(chan *Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]struct{}),
		channels:   make(map[string]*Channel),
	}
}

func (h *Hub) nickExists(nick string) bool {
	for c := range h.clients {
		if c.prefix == nick {
			return true
		}
	}
	return false
}

func (h *Hub) handleCommand(c *Client, message []byte) error {
	msg := &Message{}
	if err := json.Unmarshal(message, msg); err != nil {
		return err
	}

	if msg.Command == "NICK" {
		if h.nickExists(msg.Params) {
			return h.writeMessage(c, &Message{
				Prefix:  serverName,
				Command: "ERR_NICKNAMEINUSE",
				Params:  fmt.Sprintf("%s Nickname is already in use", msg.Params),
			})
		}

		prevPrefix := c.prefix
		c.prefix = msg.Params
		if prevPrefix == "" {
			// new connection
			h.clients[c] = struct{}{}
			log.Printf("%s connected", c.prefix)

			return h.writeMessage(c, &Message{
				Prefix:  serverName,
				Command: "RPL_WELCOME",
				Params:  fmt.Sprintf("Welcome to the Simple Golang Chat Demo Network, %s!", c.prefix),
			})
		}

		// TODO: let others in the same channel know the changes
		return nil
	}

	// other commands require a prefix
	if c.prefix == "" {
		return h.writeMessage(c, &Message{
			Prefix:  serverName,
			Command: "ERR_NOTREGISTERED",
			Params:  "You have not registered",
		})
	}

	switch msg.Command {
	case "JOIN":
		// Create a channel if needed
		ch, found := h.channels[msg.Params]
		if !found {
			log.Printf("%s created new channel '%s'", c.prefix, msg.Params)
			ch = &Channel{
				Members: map[*Client]struct{}{
					c: struct{}{},
				},
			}
			h.channels[msg.Params] = ch
		}

		// If you're already member, nothing happens.
		if found && ch.IsMember(c.prefix) {
			return nil
		}

		// Add this prefix to the channel
		ch.Members[c] = struct{}{}

		// Let others and the sender (for confirmation) in the channel know new join
		h.broadcast <- &Message{
			Prefix:  c.prefix,
			Command: msg.Command,
			Params:  msg.Params,
		}

		// Send the members in the channel including this prefix
		return h.writeMessage(c, &Message{
			Prefix:  serverName,
			Command: "RPL_NAMREPLY",
			Params:  strings.Join(ch.ListMembers(), " "),
		})

	case "PRIVMSG":
		return nil

	case "PART":
		return nil

	case "QUIT":
		return nil

	case "PONG":
		return nil

	default:
		return h.writeMessage(c, &Message{
			Prefix:  serverName,
			Command: "ERR_UNKNOWNCOMMAND",
			Params:  fmt.Sprintf("%s :Unknown command", msg.Command),
		})
	}
}

func (h *Hub) writeMessage(c *Client, message *Message) error {
	data, err := json.Marshal(message)
	if err != nil {
		return errors.Wrap(err, "failed to marshal into JSON")
	}

	select {
	case c.send <- data:
		return nil
	default:
		return errors.New("no data sent")
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = struct{}{}
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			log.Printf("BROADCAST MESSAGE: %+v", message)

			for client := range h.clients {
				if client.prefix == "" {
					continue
				}
				if err := h.writeMessage(client, message); err != nil {
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}
