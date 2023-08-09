FROM ubuntu:20.04
RUN apt-get update
RUN apt-get install supervisor -y
WORKDIR /erb
COPY erbie.conf /etc/supervisor/conf.d/
COPY erbie_log.conf /etc/supervisor/conf.d/
COPY showlog.sh /erb/ 
COPY noshowlog.sh /erb/
#COPY version /etc/
COPY erbie /erb/
#COPY start.sh /erb/
RUN mkdir -p /erb/.erbie/erbie
CMD ["/usr/bin/supervisord", "-n"]
ARG arg
ENV version=$arg

