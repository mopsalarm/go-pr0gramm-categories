FROM centurylink/ca-certs
EXPOSE 8080
COPY go-pr0gramm-categories /
ENTRYPOINT ["/go-pr0gramm-categories"]
