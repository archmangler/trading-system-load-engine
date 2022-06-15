#!/bin/bash
#QA
echo "10.0.9.118 kafka1.dexp-qa.internal" >> /etc/hosts
echo "10.0.15.240 kafka2.dexp-qa.internal" >> /etc/hosts
echo "10.0.7.192 kafka3.dexp-qa.internal" >> /etc/hosts

#PT
echo "10.0.43.66 kafka1.dexp-pt.internal" >> /etc/hosts 
echo "10.0.46.45 kafka2.dexp-pt.internal" >> /etc/hosts
echo "10.0.37.33 kafka3.dexp-pt.internal" >> /etc/hosts
