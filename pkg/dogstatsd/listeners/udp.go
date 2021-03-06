// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-2019 Datadog, Inc.

package listeners

import (
	"expvar"
	"fmt"
	"net"
	"strings"

	"github.com/DataDog/datadog-agent/pkg/util/log"

	"github.com/DataDog/datadog-agent/pkg/config"
)

var (
	udpExpvars             = expvar.NewMap("dogstatsd-udp")
	udpPacketReadingErrors = expvar.Int{}
	udpPackets             = expvar.Int{}
)

func init() {
	udpExpvars.Set("PacketReadingErrors", &udpPacketReadingErrors)
	udpExpvars.Set("Packets", &udpPackets)
}

// UDPListener implements the StatsdListener interface for UDP protocol.
// It listens to a given UDP address and sends back packets ready to be
// processed.
// Origin detection is not implemented for UDP.
type UDPListener struct {
	conn         net.PacketConn
	packetPool   *PacketPool
	packetBuffer *packetBuffer
}

// NewUDPListener returns an idle UDP Statsd listener
func NewUDPListener(packetOut chan Packets, packetPool *PacketPool) (*UDPListener, error) {
	var conn net.PacketConn
	var err error
	var url string

	if config.Datadog.GetBool("dogstatsd_non_local_traffic") == true {
		// Listen to all network interfaces
		url = fmt.Sprintf(":%d", config.Datadog.GetInt("dogstatsd_port"))
	} else {
		url = net.JoinHostPort(config.Datadog.GetString("bind_host"), config.Datadog.GetString("dogstatsd_port"))
	}

	conn, err = net.ListenPacket("udp", url)

	if rcvbuf := config.Datadog.GetInt("dogstatsd_so_rcvbuf"); rcvbuf != 0 {
		if err := conn.(*net.UDPConn).SetReadBuffer(rcvbuf); err != nil {
			return nil, fmt.Errorf("could not set socket rcvbuf: %s", err)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("can't listen: %s", err)
	}

	listener := &UDPListener{
		packetPool: packetPool,
		conn:       conn,
		packetBuffer: newPacketBuffer(uint(config.Datadog.GetInt("dogstatsd_packet_buffer_size")),
			config.Datadog.GetDuration("dogstatsd_packet_buffer_flush_timeout"), packetOut),
	}
	log.Debugf("dogstatsd-udp: %s successfully initialized", conn.LocalAddr())
	return listener, nil
}

// Listen runs the intake loop. Should be called in its own goroutine
func (l *UDPListener) Listen() {
	log.Infof("dogstatsd-udp: starting to listen on %s", l.conn.LocalAddr())
	for {
		packet := l.packetPool.Get()
		udpPackets.Add(1)
		n, _, err := l.conn.ReadFrom(packet.buffer)
		if err != nil {
			// connection has been closed
			if strings.HasSuffix(err.Error(), " use of closed network connection") {
				return
			}

			log.Errorf("dogstatsd-udp: error reading packet: %v", err)
			udpPacketReadingErrors.Add(1)
			continue
		}

		packet.Contents = packet.buffer[:n]

		// packetBuffer handles the forwarding of the packets to the dogstatsd server intake channel
		l.packetBuffer.append(packet)
	}
}

// Stop closes the UDP connection and stops listening
func (l *UDPListener) Stop() {
	l.packetBuffer.close()
	l.conn.Close()
}
