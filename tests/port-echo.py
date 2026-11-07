#!/usr/bin/env python3
import socket
import threading

def tcp_client(connection):
    with connection:
        while True:
            data = connection.recv(8192)
            if not data:
                return
            connection.sendall(data)

def tcp():
    listener = socket.socket()
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(("0.0.0.0", 18080))
    listener.listen()
    while True:
        connection, _ = listener.accept()
        threading.Thread(target=tcp_client, args=(connection,), daemon=True).start()

threading.Thread(target=tcp, daemon=True).start()
udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
udp.setsockopt(socket.IPPROTO_IP, 10, 0)  # Linux: allow IP fragmentation for the large-datagram test.
udp.bind(("0.0.0.0", 18081))
while True:
    data, address = udp.recvfrom(65535)
    print("UDP echo received:", len(data), address, flush=True)
    udp.sendto(data, address)
