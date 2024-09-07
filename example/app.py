from os import environ
from flask import Flask, flash, g, redirect, render_template, request, url_for
import requests

JWT_TOKEN = environ.get("JWT_TOKEN")

FLASH_SCOPE_API_URL = "http://127.0.0.1:5770/api/v1/user/message"

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

    @property
    def json(self) -> dict[str, str]:
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
        flash_messages.append(FlashMessage.from_json(json_flash))
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
    payload: list[dict] = [msg.json for msg in g.messages]
    requests.post(FLASH_SCOPE_API_URL, json=payload, headers=ADD_HEADERS)
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
