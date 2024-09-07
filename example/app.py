from os import environ
import json
from flask import Flask, flash, g, redirect, render_template, request, url_for
import requests

JWT_TOKEN = environ.get("JWT_TOKEN")
USE_LOCAL_FLASH_SCOPE_API = environ.get("USE_LOCAL_FLASH_SCOPE_API", "false")

FLASH_SCOPE_API_URL = "http://localhost:5770/api/v1/user/flash"

ADD_HEADERS = {
    "Authorization": f"Bearer {JWT_TOKEN}",
    "Content-Type": "application/json",
}

GET_HEADERS = {"Authorization": f"Bearer {JWT_TOKEN}"}

MESSAGES_MAP = {
    "ERROR": "Mensagem de Erro 😭 ⛔ (Error) ",
    "SUCCESS": "Mensagem de Sucesso 🎉 (SUCCESS)",
    "WARN": "Mensagem de Alerta ⚠️ (WARNING)",
    "INFO": "Mensagem de Informação ℹ️ (INFO)",
}

app = Flask(__name__)
app.secret_key = "somesecret"  # flask exige uma secret porque estamos usando a sessao


class FlashMessage:
    category: str  # in production use ENUM
    content: str

    def __init__(self, category: str, content: str):
        self.category = category
        self.content = content

    def to_json(self) -> dict[str, str]:
        return {"category": self.category, "content": self.content}

    @classmethod
    def from_json(cls, json_obj: dict):
        return FlashMessage(
            category=json_obj["category"].lower(), content=json_obj["content"]
        )


def get_flash_messages() -> list[FlashMessage]:
    r = requests.get(FLASH_SCOPE_API_URL, headers=GET_HEADERS)
    json_flashes = r.json()

    flash_messages: list[FlashMessage] = []
    for json_flash in json_flashes:
        if (json_flash["type"] != "MESSAGE"):
            # se nao é uma mensagem flash apenas ignoramos
            continue
        # o json (payload) vem como string (aspas escapadas) dentro do atributo "content"
        flash_message_payload: dict = json.loads(json_flash["content"])
        flash_messages.append(FlashMessage.from_json(flash_message_payload))
    return flash_messages


def add_flash_messages(*messages: FlashMessage):
    # 'g' é um objeto global com escopo de request
    if "messages" not in g:
        g.messages = []

    g.messages.extend(messages)


@app.before_request
def input_flash_messages():
    messages = get_flash_messages()
    for message in messages:
        flash(category=message.category, message=message.content)


@app.after_request
def send_flash_messages_to_api(response):
    if "messages" not in g:  # no messages to add
        return response
    json_flashes: list[dict] = []
    message: FlashMessage
    for message in g.messages:
        payload: dict = {"type": "MESSAGE", "content": json.dumps(message.to_json())}
        json_flashes.append(payload)
    requests.post(FLASH_SCOPE_API_URL, json=json_flashes, headers=ADD_HEADERS)
    return response


@app.get("/")
def index():
    return render_template("index.html")


@app.get("/test")
def test_get():
    return render_template("test.html")


@app.post("/test")
def test_post():
    category = request.form["message_type"]
    content = MESSAGES_MAP[category]
    flash_message = FlashMessage(category=category, content=content)
    add_flash_messages(flash_message)

    return redirect(url_for("index"))


if __name__ == "__main__":
    if not JWT_TOKEN:
        print("Missing JWT_TOKEN env variable")
        exit(1)
    
    app.run(port=5000, debug=False)
