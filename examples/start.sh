nohup ./node1.sh 2>>data/n1.log &
sleep 0.3;
nohup ./node2.sh 2>>data/n2.log &
sleep 0.3;
nohup ./node3.sh 2>>data/n3.log &
sleep 0.3;
nohup ./node4.sh 2>>data/n4.log &
sleep 0.3;
