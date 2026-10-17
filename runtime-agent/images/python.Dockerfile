FROM python:3.13-slim-bookworm
COPY requirements-kernel.txt /tmp/requirements-kernel.txt
RUN pip install --no-cache-dir -r /tmp/requirements-kernel.txt \
    && rm /tmp/requirements-kernel.txt
COPY images/jupyter_server_config.py /etc/cellestial/jupyter_server_config.py
USER 65532:65532
WORKDIR /workspace
CMD ["jupyter", "server", "--config=/etc/cellestial/jupyter_server_config.py"]
