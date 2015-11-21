FROM busybox
COPY check.sh /

RUN mount

RUN echo layer1 > layer1-file1 ; ln layer1-file1 layer1-file2 ; ln layer1-file1 layer1-file3
RUN echo layer2 > layer2-file1 ; ln layer2-file1 layer2-file2 ; ln layer2-file1 layer2-file3
RUN echo layer3 > layer3-file1 ; ln layer3-file1 layer3-file2 ; ln layer3-file1 layer3-file3
RUN rm -f layer1-file1 layer2-file2 layer3-file3

RUN mount

RUN echo OLD > layer4-file1 ; ln layer4-file1 layer4-file2 ; ln layer4-file1 layer4-file3
RUN echo NEW > layer4-file1 ; ln -f layer4-file1 layer4-file2 ; ln -f layer4-file1 layer4-file3 ; echo foo > foo

RUN echo line1 >  layer5-file1 ; ln layer5-file1 layer5-file2 ; ln layer5-file1 layer5-file3
RUN echo line2 >> layer5-file1

CMD /check.sh 2>&1
