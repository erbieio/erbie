FROM ubuntu:20.04
RUN apt-get update
RUN apt-get install supervisor -y
WORKDIR /wm
COPY erbie.conf /etc/supervisor/conf.d/
COPY erbie_log.conf /etc/supervisor/conf.d/
COPY showlog.sh /wm/ 
COPY noshowlog.sh /wm/
#COPY version /etc/
COPY erbie /wm/
#COPY start.sh /wm/
RUN mkdir -p /wm/.erbie/erbie
CMD ["/usr/bin/supervisord", "-n"]
ARG arg
ENV version=$arg

