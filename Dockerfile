FROM centurylink/ca-certs
ADD tasque /usr/bin/
ENTRYPOINT ["/usr/bin/tasque"]
