FROM golang:1.13.8 as build
LABEL stage=build

# Build WMCB
RUN mkdir /build/
WORKDIR /build/
COPY . .

RUN make build-wmcb-unit-test
RUN make build-wmcb-e2e-test

FROM golang:1.13.8 as testing
LABEL stage=testing

WORKDIR /home/test
COPY /internal/test .

COPY --from=build /build/wmcb_unit_test.exe .
COPY --from=build /build/wmcb_e2e_test.exe .

ENTRYPOINT ["wmcb/runTest.sh"]