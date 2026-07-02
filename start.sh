#!/bin/sh

echo "Starting Backend1..."
./backend1 &

echo "Starting Backend2..."
./backend2 &

echo "Starting Load Balancer..."
exec ./lb