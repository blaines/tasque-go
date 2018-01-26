FROM centurylink/ca-certs
ADD tasque /usr/bin/
CMD ["/usr/bin/tasque"]
