Host	CPUs	RAM	Accelerator	Graphics

dh1
2× Intel Xeon Gold 6326, 16 cores each, 64 threads, 2.90 GHz
128 GB
AMD Pensando DSC2-100
Matrox G200 (console only)

dh2
2× Intel Xeon Gold 6326, 16 cores each, 64 threads, 2.90 GHz
256 GB
NVIDIA BlueField-2
Matrox G200 (console only)

dh3
1× Arm Neoverse-N1, 80 cores, 80 threads
256 GB
Marvell CN106
Matrox G200 (console only)

dh9
2× AMD EPYC 9354, 32 cores each, 128 threads
384 GB
none seen on the bus
ASPEED (console only)



Host	Address	What is on it	This repo?
dh2
172.22.1.2
k3s. BlueField-2. This is the only machine running the adapter.
Yes

dh1 (opi)
172.22.1.1
AMD Pensando card. No Kubernetes.
No

dh3
172.22.1.3
Marvell card. No Kubernetes.
No
